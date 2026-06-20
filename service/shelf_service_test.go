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
