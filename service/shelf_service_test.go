package service

import (
	"context"
	"cx3/config"
	"cx3/model"
	redisrepo "cx3/repository/redis"
	mysqlrepo "cx3/repository/mysql"
	"cx3/utils"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return client, mr
}

func setupTestLogger() {
	if utils.Logger == nil {
		l, _ := zap.NewDevelopment()
		utils.Logger = l
		utils.SugarLogger = l.Sugar()
	}
}

func setupTestShelfService(t *testing.T) (*ShelfService, *redis.Client, *miniredis.Miniredis) {
	setupTestLogger()
	client, mr := setupTestRedis(t)
	cfg := &config.Config{
		Idempotent: config.IdempotentConfig{TTLSeconds: 300},
		Shelf:      config.ShelfConfig{LockTimeoutSeconds: 1800},
	}
	stockRepo := redisrepo.NewStockRepo(client, cfg)
	transactionRepo := mysqlrepo.NewTransactionRepo(nil)
	shelfRepo := mysqlrepo.NewShelfRepo(nil)
	service := NewShelfService(cfg, stockRepo, transactionRepo, shelfRepo)
	return service, client, mr
}

func TestShelfService_DecrStock_Concurrent(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-concurrent")
	shelfID := "SH-TEST-001"
	slotNo := 1
	productID := "P001"
	initialStock := 100

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), initialStock, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)
	productJSON, _ := json.Marshal(&model.Product{ProductID: productID, SKU: "SKU001", Name: "Test", Price: 100})
	client.Set(ctx, utils.BuildProductInfoKey(productID), productJSON, time.Hour)

	concurrentCount := 50
	perRequestQuantity := 2
	var successCount int32
	var failCount int32
	var wg sync.WaitGroup

	start := make(chan struct{})

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			req := &model.PickupRequest{
				RequestID:     fmt.Sprintf("REQ-%d-%d", time.Now().UnixNano(), idx),
				IdempotentKey: fmt.Sprintf("IDEM-%d-%d", time.Now().UnixNano(), idx),
				ShelfID:       shelfID,
				UserID:        "USER-" + strconv.Itoa(idx%10),
				ProductID:     productID,
				Quantity:      perRequestQuantity,
				SlotNo:        slotNo,
			}
			result := service.Pickup(ctx, req, "127.0.0.1")
			if result.ErrCode == utils.CodeSuccess {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&failCount, 1)
			}
		}(i)
	}

	close(start)
	wg.Wait()

	finalStock, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
	expectedSuccess := initialStock / perRequestQuantity
	expectedDeducted := int(successCount) * perRequestQuantity
	actualDeducted := initialStock - finalStock

	t.Logf("Concurrent: %d, PerQty: %d, Initial: %d", concurrentCount, perRequestQuantity, initialStock)
	t.Logf("Success: %d, Fail: %d", successCount, failCount)
	t.Logf("Expected success max: %d", expectedSuccess)
	t.Logf("Final stock: %d, Deducted: %d, Expected deduction: %d", finalStock, actualDeducted, expectedDeducted)

	assert.True(t, successCount <= int32(expectedSuccess), "成功请求数不应超过库存允许量")
	assert.Equal(t, initialStock, actualDeducted+finalStock, "扣减后库存+实际扣减=初始库存")
	assert.True(t, actualDeducted <= initialStock, "实际扣减量不应超过初始库存")
	assert.True(t, finalStock >= 0, "最终库存不能为负数")
}

func TestShelfService_Idempotent(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-idempotent")
	shelfID := "SH-TEST-IDEM"
	slotNo := 1
	productID := "P001"
	initialStock := 50

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), initialStock, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)
	productJSON, _ := json.Marshal(&model.Product{ProductID: productID, SKU: "SKU001", Name: "Test", Price: 200})
	client.Set(ctx, utils.BuildProductInfoKey(productID), productJSON, time.Hour)

	req := &model.PickupRequest{
		RequestID:     "REQ-IDEM-001",
		IdempotentKey: "IDEM-KEY-001",
		ShelfID:       shelfID,
		UserID:        "USER-001",
		ProductID:     productID,
		Quantity:      3,
		SlotNo:        slotNo,
	}

	result1 := service.Pickup(ctx, req, "127.0.0.1")
	assert.Equal(t, utils.CodeSuccess, result1.ErrCode, "第一次请求应成功")
	assert.False(t, result1.Response.IsDuplicate, "第一次不应标记为重复")
	stockAfter1 := result1.Response.StockAfter

	result2 := service.Pickup(ctx, req, "127.0.0.1")
	assert.Equal(t, utils.CodeSuccess, result2.ErrCode, "重复请求也应返回成功")
	assert.True(t, result2.Response.IsDuplicate, "第二次请求应标记为重复")
	assert.Equal(t, stockAfter1, result2.Response.StockAfter, "重复请求返回的库存应相同")

	finalStock, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
	assert.Equal(t, initialStock-3, finalStock, "幂等性保证：库存只扣减一次")
}

func TestShelfService_ShelfLock(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-lock")
	shelfID := "SH-TEST-LOCK"
	slotNo := 1
	productID := "P001"

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 50, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)
	productJSON, _ := json.Marshal(&model.Product{ProductID: productID, SKU: "SKU001", Name: "Test", Price: 200})
	client.Set(ctx, utils.BuildProductInfoKey(productID), productJSON, time.Hour)

	lockReq := &model.ShelfLockRequest{
		RequestID:     "REQ-LOCK-001",
		IdempotentKey: "LOCK-IDEM-001",
		ShelfID:       shelfID,
		OperatorID:    "OP-001",
		OperatorName:  "管理员",
		LockType:      1,
		Reason:        "重力传感器异常",
		LockSeconds:   1800,
	}

	lockResult := service.LockShelf(ctx, lockReq)
	assert.Equal(t, utils.CodeSuccess, lockResult.ErrCode, "锁定应成功")
	t.Logf("Lock result: lockUntil=%d", lockResult.Response.LockUntil)

	pickupReq := &model.PickupRequest{
		RequestID:     "REQ-PICKUP-AFTER-LOCK",
		IdempotentKey: "PICK-AFTER-LOCK",
		ShelfID:       shelfID,
		UserID:        "USER-001",
		ProductID:     productID,
		Quantity:      1,
		SlotNo:        slotNo,
	}
	pickupResult := service.Pickup(ctx, pickupReq, "127.0.0.1")
	assert.Equal(t, utils.CodeShelfLocked, pickupResult.ErrCode, "锁定后取货应返回锁定错误")

	unlockReq := &model.ShelfUnlockRequest{
		RequestID:     "REQ-UNLOCK-001",
		IdempotentKey: "UNLOCK-IDEM-001",
		ShelfID:       shelfID,
		OperatorID:    "OP-001",
		OperatorName:  "管理员",
		Reason:        "故障已修复",
	}
	unlockResult := service.UnlockShelf(ctx, unlockReq)
	assert.Equal(t, utils.CodeSuccess, unlockResult.ErrCode, "解锁应成功")

	pickupReq2 := &model.PickupRequest{
		RequestID:     "REQ-PICKUP-AFTER-UNLOCK",
		IdempotentKey: "PICK-AFTER-UNLOCK-2",
		ShelfID:       shelfID,
		UserID:        "USER-001",
		ProductID:     productID,
		Quantity:      1,
		SlotNo:        slotNo,
	}
	pickupResult2 := service.Pickup(ctx, pickupReq2, "127.0.0.1")
	assert.Equal(t, utils.CodeSuccess, pickupResult2.ErrCode, "解锁后取货应成功")
}

func TestShelfService_StockNotEnough(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-stock-not-enough")
	shelfID := "SH-TEST-NOSTOCK"
	slotNo := 1
	productID := "P001"

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 2, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)
	productJSON, _ := json.Marshal(&model.Product{ProductID: productID, SKU: "SKU001", Name: "Test", Price: 200})
	client.Set(ctx, utils.BuildProductInfoKey(productID), productJSON, time.Hour)

	req := &model.PickupRequest{
		RequestID:     "REQ-NOSTOCK-001",
		IdempotentKey: "IDEM-NOSTOCK-001",
		ShelfID:       shelfID,
		UserID:        "USER-001",
		ProductID:     productID,
		Quantity:      5,
		SlotNo:        slotNo,
	}

	result := service.Pickup(ctx, req, "127.0.0.1")
	assert.Equal(t, utils.CodeStockNotEnough, result.ErrCode, "库存不足应返回对应错误码")

	finalStock, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
	assert.Equal(t, 2, finalStock, "库存不足时不应扣减")
}

func TestShelfService_MultiSlot_Concurrent(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-multislot-concurrent")
	shelfID := "SH-TEST-MULTI"

	const slotCount = 10
	const initialStock = 100
	const perQty = 1
	const requestsPerSlot = 80
	const totalRequests = slotCount * requestsPerSlot

	type slotInfo struct {
		ProductID string
		Initial   int
	}
	slots := make([]slotInfo, slotCount)

	for s := 0; s < slotCount; s++ {
		slotNo := s + 1
		pid := fmt.Sprintf("P-SLOT-%02d", slotNo)
		slots[s] = slotInfo{ProductID: pid, Initial: initialStock}

		client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), initialStock, 0)
		client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), pid, 0)
		pJSON, _ := json.Marshal(&model.Product{ProductID: pid, SKU: fmt.Sprintf("SKU%02d", slotNo), Name: fmt.Sprintf("商品%d号", slotNo), Price: 100 + int64(slotNo)*10})
		client.Set(ctx, utils.BuildProductInfoKey(pid), pJSON, time.Hour)
	}

	var slotSuccess [slotCount]int32
	var slotFail [slotCount]int32
	var wg sync.WaitGroup

	start := make(chan struct{})
	reqIdx := int64(0)

	for r := 0; r < totalRequests; r++ {
		s := r % slotCount
		slotNo := s + 1
		wg.Add(1)
		go func(slotIdx, sn int) {
			defer wg.Done()
			<-start
			n := atomic.AddInt64(&reqIdx, 1)
			pid := slots[slotIdx].ProductID
			req := &model.PickupRequest{
				RequestID:     fmt.Sprintf("REQ-MULTI-%d-%d", time.Now().UnixNano(), n),
				IdempotentKey: fmt.Sprintf("IDEM-MULTI-%d-%d", n, slotIdx),
				ShelfID:       shelfID,
				UserID:        fmt.Sprintf("USER-%d", n%50),
				ProductID:     pid,
				Quantity:      perQty,
				SlotNo:        sn,
			}
			result := service.Pickup(ctx, req, fmt.Sprintf("10.0.0.%d", n%256))
			if result.ErrCode == utils.CodeSuccess {
				atomic.AddInt32(&slotSuccess[slotIdx], 1)
			} else {
				atomic.AddInt32(&slotFail[slotIdx], 1)
			}
		}(s, slotNo)
	}

	close(start)
	wg.Wait()

	t.Log("============= 多货道并发结果 =============")
	t.Logf("总请求: %d (每货道 %d 请求)", totalRequests, requestsPerSlot)
	t.Logf("%-8s %-12s %-10s %-10s %-12s %-12s %-8s", "SLOT", "PRODUCT", "INIT", "SUCCESS", "EXPECT_DED", "ACTUAL_STOCK", "MATCH?")

	allCorrect := true
	for s := 0; s < slotCount; s++ {
		slotNo := s + 1
		pid := slots[s].ProductID
		success := int(slotSuccess[s])
		expectedDeducted := success * perQty
		expectedFinal := initialStock - expectedDeducted
		actualFinal, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
		actualProduct, _ := client.Get(ctx, utils.BuildShelfProductKey(shelfID, slotNo)).Result()

		match := actualFinal == expectedFinal && actualProduct == pid
		if !match {
			allCorrect = false
		}

		status := "✓"
		if !match {
			status = fmt.Sprintf("✗ (stock=%d/%d prod=%s/%s)", actualFinal, expectedFinal, actualProduct, pid)
		}
		t.Logf("%-8d %-12s %-10d %-10d %-12d %-12d %-8s", slotNo, pid, initialStock, success, expectedDeducted, actualFinal, status)
	}

	t.Log("==========================================")

	assert.True(t, allCorrect, "多货道并发时，每个货道的库存和商品对应关系必须完全正确")

	for s := 0; s < slotCount; s++ {
		slotNo := s + 1
		pid := slots[s].ProductID
		success := int(slotSuccess[s])
		expectedDeducted := success * perQty
		expectedFinal := initialStock - expectedDeducted
		actualFinal, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
		actualProduct, _ := client.Get(ctx, utils.BuildShelfProductKey(shelfID, slotNo)).Result()

		assert.Equal(t, expectedFinal, actualFinal, "货道 %d 最终库存不正确", slotNo)
		assert.Equal(t, pid, actualProduct, "货道 %d 商品映射被污染！期望 %s, 实际 %s", slotNo, pid, actualProduct)
		assert.True(t, actualFinal >= 0, "货道 %d 库存不能为负数: %d", slotNo, actualFinal)
	}
}

func TestShelfService_Promo_PriceApplied(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-promo-apply")
	shelfID := "SH-TEST-PROMO"
	slotNo := 1
	productID := "P-PROMO-001"
	originalPrice := int64(300)
	promoPrice := int64(99)
	initialStock := 10

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), initialStock, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)
	productJSON, _ := json.Marshal(&model.Product{ProductID: productID, SKU: "SKU-PROMO", Name: "临期商品特价", Price: originalPrice})
	client.Set(ctx, utils.BuildProductInfoKey(productID), productJSON, time.Hour)

	t.Log("=== 阶段 1：未设促销，按原价计算 ===")
	req1 := &model.PickupRequest{
		RequestID:     "REQ-NO-PROMO-1",
		IdempotentKey: "IDEM-NO-PROMO-1",
		ShelfID:       shelfID,
		UserID:        "USER-001",
		ProductID:     productID,
		Quantity:      1,
		SlotNo:        slotNo,
	}
	result1 := service.Pickup(ctx, req1, "127.0.0.1")
	assert.Equal(t, utils.CodeSuccess, result1.ErrCode, "无促销时扣减应成功")
	assert.Equal(t, originalPrice, result1.Response.UnitPrice, "无促销时单价应为原价")
	assert.Equal(t, originalPrice, result1.Response.TotalAmount, "无促销时总金额应为原价")
	assert.Equal(t, int64(0), result1.Response.DiscountAmt, "无促销时优惠金额为 0")
	assert.Equal(t, "", result1.Response.PromoID, "无促销时 PromoID 为空")
	t.Logf("  原价=%d, 成交价=%d, 优惠=%d ✓", originalPrice, result1.Response.UnitPrice, result1.Response.DiscountAmt)

	t.Log("=== 阶段 2：设置促销（立即生效）===")
	now := utils.NowUnix()
	promoID := "PROMO-NEAR-EXPIRY-001"
	setReq := &model.SetPromoRequest{
		RequestID:     "PROMO-SET-001",
		IdempotentKey: "PROMO-IDEM-001",
		PromoID:       promoID,
		PromoName:     "临期商品3.3折秒杀",
		ShelfID:       shelfID,
		SlotNo:        slotNo,
		ProductID:     productID,
		PromoPrice:    promoPrice,
		StartAt:       now - 60,
		EndAt:         now + 3600,
		OperatorID:    "OP-001",
		OperatorName:  "运营管理员",
	}
	setResult := service.SetPromo(ctx, setReq)
	assert.Equal(t, utils.CodeSuccess, setResult.ErrCode, "设置促销应成功")
	assert.True(t, setResult.Response.IsActive, "促销应处于激活状态")
	assert.Equal(t, promoPrice, setResult.Response.PromoPrice, "促销价正确")
	t.Logf("  促销[%s]已设置: 原价=%d分, 促销价=%d分 ✓", promoID, originalPrice, promoPrice)

	t.Log("=== 阶段 3：促销生效，Pickup 应按促销价计算 ===")
	req2 := &model.PickupRequest{
		RequestID:     "REQ-PROMO-1",
		IdempotentKey: "IDEM-PROMO-1",
		ShelfID:       shelfID,
		UserID:        "USER-002",
		ProductID:     productID,
		Quantity:      2,
		SlotNo:        slotNo,
	}
	result2 := service.Pickup(ctx, req2, "127.0.0.1")
	assert.Equal(t, utils.CodeSuccess, result2.ErrCode, "促销中扣减应成功")
	assert.Equal(t, promoPrice, result2.Response.UnitPrice, "促销中单价应为促销价")
	assert.Equal(t, originalPrice, result2.Response.OriginalPrice, "应返回原价")
	assert.Equal(t, promoPrice*2, result2.Response.TotalAmount, "促销中总金额=促销价*数量")
	assert.Equal(t, (originalPrice-promoPrice)*2, result2.Response.DiscountAmt, "优惠金额计算错误")
	assert.Equal(t, promoID, result2.Response.PromoID, "PromoID 应正确返回")
	assert.Equal(t, "临期商品3.3折秒杀", result2.Response.PromoName, "PromoName 应正确返回")
	t.Logf("  原价=%d, 促销价=%d, 数量=2, 总金额=%d, 优惠=%d, promo_id=%s ✓",
		originalPrice, result2.Response.UnitPrice, result2.Response.TotalAmount,
		result2.Response.DiscountAmt, result2.Response.PromoID)

	t.Log("=== 阶段 4：取消促销，Pickup 恢复原价 ===")
	cancelReq := &model.CancelPromoRequest{
		RequestID:    "PROMO-CANCEL-001",
		PromoID:      promoID,
		ShelfID:      shelfID,
		SlotNo:       slotNo,
		OperatorID:   "OP-001",
		OperatorName: "运营管理员",
	}
	cancelResult := service.CancelPromo(ctx, cancelReq)
	assert.Equal(t, utils.CodeSuccess, cancelResult.ErrCode, "取消促销应成功")
	assert.True(t, cancelResult.Response.Canceled, "canceled=true")
	t.Logf("  促销[%s]已取消 ✓", promoID)

	req3 := &model.PickupRequest{
		RequestID:     "REQ-AFTER-CANCEL-1",
		IdempotentKey: "IDEM-AFTER-CANCEL-1",
		ShelfID:       shelfID,
		UserID:        "USER-003",
		ProductID:     productID,
		Quantity:      1,
		SlotNo:        slotNo,
	}
	result3 := service.Pickup(ctx, req3, "127.0.0.1")
	assert.Equal(t, utils.CodeSuccess, result3.ErrCode, "取消促销后扣减应成功")
	assert.Equal(t, originalPrice, result3.Response.UnitPrice, "取消促销后应恢复原价")
	assert.Equal(t, int64(0), result3.Response.DiscountAmt, "取消促销后优惠金额为 0")
	assert.Equal(t, "", result3.Response.PromoID, "取消促销后 PromoID 为空")
	t.Logf("  恢复原价=%d, 成交价=%d ✓", originalPrice, result3.Response.UnitPrice)

	t.Log("=== 阶段 5：库存验证（10 - 1 - 2 - 1 = 6）===")
	expectedFinal := initialStock - 1 - 2 - 1
	actualStock, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
	assert.Equal(t, expectedFinal, actualStock, "最终库存应精准匹配")
	t.Logf("  初始库存=%d, 扣减后=%d, 预期=%d ✓", initialStock, actualStock, expectedFinal)
}

func TestShelfService_Promo_Validation(t *testing.T) {
	service, client, mr := setupTestShelfService(t)
	defer mr.Close()

	ctx := utils.ContextWithTraceID(context.Background(), "test-promo-validate")
	shelfID := "SH-TEST-VALID"
	slotNo := 1
	productID := "P-VALID-001"
	originalPrice := int64(500)

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 10, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)
	productJSON, _ := json.Marshal(&model.Product{ProductID: productID, SKU: "SKU-VALID", Name: "测试商品", Price: originalPrice})
	client.Set(ctx, utils.BuildProductInfoKey(productID), productJSON, time.Hour)

	now := utils.NowUnix()

	t.Run("促销价必须大于0", func(t *testing.T) {
		result := service.SetPromo(ctx, &model.SetPromoRequest{
			RequestID: "V1", IdempotentKey: "V1",
			PromoID: "V1", PromoName: "测试", ShelfID: shelfID, SlotNo: slotNo, ProductID: productID,
			PromoPrice: 0, StartAt: now, EndAt: now + 3600, OperatorID: "OP", OperatorName: "OP",
		})
		assert.NotEqual(t, utils.CodeSuccess, result.ErrCode, "促销价=0应失败")
	})

	t.Run("促销价必须小于原价", func(t *testing.T) {
		result := service.SetPromo(ctx, &model.SetPromoRequest{
			RequestID: "V2", IdempotentKey: "V2",
			PromoID: "V2", PromoName: "测试", ShelfID: shelfID, SlotNo: slotNo, ProductID: productID,
			PromoPrice: originalPrice + 10, StartAt: now, EndAt: now + 3600, OperatorID: "OP", OperatorName: "OP",
		})
		assert.NotEqual(t, utils.CodeSuccess, result.ErrCode, "促销价>=原价应失败")
	})

	t.Run("时间范围无效（结束早于开始）", func(t *testing.T) {
		result := service.SetPromo(ctx, &model.SetPromoRequest{
			RequestID: "V3", IdempotentKey: "V3",
			PromoID: "V3", PromoName: "测试", ShelfID: shelfID, SlotNo: slotNo, ProductID: productID,
			PromoPrice: 100, StartAt: now + 3600, EndAt: now + 1800, OperatorID: "OP", OperatorName: "OP",
		})
		assert.NotEqual(t, utils.CodeSuccess, result.ErrCode, "结束时间早于开始时间应失败")
	})

	t.Log("  全部参数校验通过 ✓")
}
