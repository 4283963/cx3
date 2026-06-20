package service

import (
	"context"
	"cx3/config"
	"cx3/model"
	redisrepo "cx3/repository/redis"
	mysqlrepo "cx3/repository/mysql"
	"cx3/utils"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

var (
	ErrShelfLocked     = errors.New("shelf is locked")
	ErrShelfOffline    = errors.New("shelf is offline")
	ErrStockNotEnough  = errors.New("stock not enough")
	ErrProductNotFound = errors.New("product not found")
	ErrSlotMismatch    = errors.New("slot and product mismatch")
	ErrShelfNotFound   = errors.New("shelf not found")
)

type ShelfService struct {
	cfg              *config.Config
	stockRepo        *redisrepo.StockRepo
	transactionRepo  *mysqlrepo.TransactionRepo
	shelfRepo        *mysqlrepo.ShelfRepo
}

func NewShelfService(cfg *config.Config, stockRepo *redisrepo.StockRepo,
	transactionRepo *mysqlrepo.TransactionRepo, shelfRepo *mysqlrepo.ShelfRepo) *ShelfService {
	return &ShelfService{
		cfg:             cfg,
		stockRepo:       stockRepo,
		transactionRepo: transactionRepo,
		shelfRepo:       shelfRepo,
	}
}

type PickupResult struct {
	Response    *model.PickupResponse
	ErrCode     int
	Err         error
}

func (s *ShelfService) Pickup(ctx context.Context, req *model.PickupRequest, clientIP string) *PickupResult {
	traceID := utils.TraceIDFromContext(ctx)

	var duplicateTx *model.Transaction
	if !reqFromDuplicateKey(ctx, s.transactionRepo, req.IdempotentKey, &duplicateTx) {
		return &PickupResult{
			Response: buildPickupResponseFromTx(duplicateTx),
			ErrCode:  utils.CodeSuccess,
		}
	}

	product, err := s.getProductWithCache(ctx, req.ProductID)
	if err != nil {
		utils.SugarLogger.Errorw("get product failed",
			zap.String("trace_id", traceID),
			zap.String("product_id", req.ProductID),
			zap.Error(err),
		)
		return &PickupResult{
			ErrCode: utils.CodeInternalError,
			Err:     errors.New("服务异常"),
		}
	}
	if product == nil {
		return &PickupResult{
			ErrCode: utils.CodeProductNotFound,
			Err:     ErrProductNotFound,
		}
	}

	if !reqFromMysqlSlotCheck(ctx, s.shelfRepo, req.ShelfID, req.SlotNo, req.ProductID) {
		return &PickupResult{
			ErrCode: utils.CodeSlotMismatch,
			Err:     ErrSlotMismatch,
		}
	}

	redisSlotProduct, _ := s.stockRepo.GetShelfProduct(ctx, req.ShelfID, req.SlotNo)
	if redisSlotProduct != "" && redisSlotProduct != req.ProductID {
		utils.SugarLogger.Errorw("REDIS SLOT-PRODUCT MISMATCH DETECTED",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Int("slot_no", req.SlotNo),
			zap.String("expected_product", req.ProductID),
			zap.String("actual_redis_product", redisSlotProduct),
		)
		return &PickupResult{
			ErrCode: utils.CodeSlotMismatch,
			Err:     fmt.Errorf("%w: Redis货道商品不匹配: 期望=%s 实际=%s", ErrSlotMismatch, req.ProductID, redisSlotProduct),
		}
	}

	decrResult, err := s.stockRepo.DecrStock(ctx, req.ShelfID, req.SlotNo, req.ProductID,
		req.Quantity, req.IdempotentKey)
	if err != nil {
		utils.SugarLogger.Errorw("decr stock failed",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &PickupResult{
			ErrCode: utils.CodeServiceUnavailable,
			Err:     errors.New("库存服务暂不可用"),
		}
	}

	if decrResult.IsDuplicate {
		existingTx, _ := s.transactionRepo.GetByIdempotentKey(ctx, req.IdempotentKey)
		if existingTx != nil {
			return &PickupResult{
				Response: buildPickupResponseFromTx(existingTx),
				ErrCode:  utils.CodeSuccess,
			}
		}
		return &PickupResult{
			Response: &model.PickupResponse{
				ShelfID:     req.ShelfID,
				ProductID:   req.ProductID,
				Quantity:    req.Quantity,
				StockBefore: decrResult.StockBefore,
				StockAfter:  decrResult.StockAfter,
				UnitPrice:   product.Price,
				TotalAmount: product.Price * int64(req.Quantity),
				PickupAt:    utils.NowUnixMilli(),
				IsDuplicate: true,
			},
			ErrCode: utils.CodeSuccess,
		}
	}

	switch decrResult.ErrCode {
	case 1:
		luaExpectedDeduct := decrResult.StockBefore - decrResult.StockAfter
		if luaExpectedDeduct != req.Quantity {
			utils.SugarLogger.Fatalw("LUA DEDUCT AMOUNT MISMATCH",
				zap.String("trace_id", traceID),
				zap.String("shelf_id", req.ShelfID),
				zap.Int("slot_no", req.SlotNo),
				zap.String("product_id", req.ProductID),
				zap.Int("requested_qty", req.Quantity),
				zap.Int("lua_stock_before", decrResult.StockBefore),
				zap.Int("lua_stock_after", decrResult.StockAfter),
				zap.Int("lua_deduct", luaExpectedDeduct),
			)
			return &PickupResult{
				ErrCode: utils.CodeInternalError,
				Err:     fmt.Errorf("致命错误: Lua扣减量=%d, 请求量=%d", luaExpectedDeduct, req.Quantity),
			}
		}

		postStock, getErr := s.stockRepo.GetStock(ctx, req.ShelfID, req.SlotNo)
		if getErr != nil {
			utils.SugarLogger.Errorw("post-check get stock failed",
				zap.String("trace_id", traceID),
				zap.String("shelf_id", req.ShelfID),
				zap.Int("slot_no", req.SlotNo),
				zap.Error(getErr),
			)
		} else {
			if postStock < 0 {
				utils.SugarLogger.Fatalw("NEGATIVE STOCK DETECTED",
					zap.String("trace_id", traceID),
					zap.String("shelf_id", req.ShelfID),
					zap.Int("slot_no", req.SlotNo),
					zap.String("product_id", req.ProductID),
					zap.Int("requested_qty", req.Quantity),
					zap.Int("lua_stock_after", decrResult.StockAfter),
					zap.Int("redis_post_stock", postStock),
				)
				return &PickupResult{
					ErrCode: utils.CodeInternalError,
					Err:     fmt.Errorf("致命错误: 货道%d出现负库存=%d", req.SlotNo, postStock),
				}
			}
			if postStock > decrResult.StockAfter {
				utils.SugarLogger.Fatalw("STOCK INCREASED AFTER DEDUCT",
					zap.String("trace_id", traceID),
					zap.String("shelf_id", req.ShelfID),
					zap.Int("slot_no", req.SlotNo),
					zap.String("product_id", req.ProductID),
					zap.Int("requested_qty", req.Quantity),
					zap.Int("lua_stock_after", decrResult.StockAfter),
					zap.Int("redis_post_stock", postStock),
				)
				return &PickupResult{
					ErrCode: utils.CodeInternalError,
					Err:     fmt.Errorf("致命错误: 扣减后库存反而增加: Lua返回=%d, Redis实际=%d", decrResult.StockAfter, postStock),
				}
			}
		}

		postProduct, getErr2 := s.stockRepo.GetShelfProduct(ctx, req.ShelfID, req.SlotNo)
		if getErr2 == nil && postProduct != "" && postProduct != req.ProductID {
			utils.SugarLogger.Fatalw("PRODUCT MAPPING CORRUPTED",
				zap.String("trace_id", traceID),
				zap.String("shelf_id", req.ShelfID),
				zap.Int("slot_no", req.SlotNo),
				zap.String("expected_product", req.ProductID),
				zap.String("actual_redis_product", postProduct),
			)
			return &PickupResult{
				ErrCode: utils.CodeInternalError,
				Err:     fmt.Errorf("致命映射错误: 货道%d应存放=%s, Redis实际=%s", req.SlotNo, req.ProductID, postProduct),
			}
		}
	case -1:
		return &PickupResult{
			ErrCode: utils.CodeStockNotEnough,
			Err:     fmt.Errorf("%w: 当前库存=%s", ErrStockNotEnough, decrResult.PrevTxResult),
		}
	case -3:
		return &PickupResult{
			ErrCode: utils.CodeShelfLocked,
			Err:     ErrShelfLocked,
		}
	case -4:
		return &PickupResult{
			ErrCode: utils.CodeSlotMismatch,
			Err:     ErrSlotMismatch,
		}
	default:
		return &PickupResult{
			ErrCode: utils.CodeInternalError,
			Err:     errors.New("未知错误"),
		}
	}

	_, _ = s.stockRepo.IncrETag(ctx, req.ShelfID)

	unitPrice := product.Price
	originalPrice := product.Price
	discountAmt := int64(0)
	promoID := ""
	promoName := ""

	promoResult, promoErr := s.stockRepo.GetPromo(ctx, req.ShelfID, req.SlotNo)
	if promoErr != nil {
		utils.SugarLogger.Warnw("get promo failed, fallback to original price",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Int("slot_no", req.SlotNo),
			zap.Error(promoErr),
		)
	}
	if promoResult != nil && promoResult.Found && promoResult.Active && promoResult.Promo != nil {
		if promoResult.Promo.ProductID == "" || promoResult.Promo.ProductID == req.ProductID {
			if promoResult.Promo.PromoPrice > 0 && promoResult.Promo.PromoPrice < product.Price {
				unitPrice = promoResult.Promo.PromoPrice
				promoID = promoResult.Promo.PromoID
				promoName = promoResult.Promo.PromoName
				discountAmt = (product.Price - unitPrice) * int64(req.Quantity)
				utils.SugarLogger.Infow("promo applied",
					zap.String("trace_id", traceID),
					zap.String("promo_id", promoID),
					zap.String("promo_name", promoName),
					zap.Int64("original_price", originalPrice),
					zap.Int64("promo_price", unitPrice),
					zap.Int64("discount", discountAmt),
				)
			}
		}
	}

	totalAmount := unitPrice * int64(req.Quantity)

	txID := utils.GenerateTransactionID()
	tx := &model.Transaction{
		TransactionID: txID,
		OrderID:       req.OrderID,
		UserID:        req.UserID,
		ShelfID:       req.ShelfID,
		ProductID:     req.ProductID,
		SKU:           product.SKU,
		ProductName:   product.Name,
		SlotNo:        req.SlotNo,
		Quantity:      req.Quantity,
		UnitPrice:     unitPrice,
		OriginalPrice: originalPrice,
		TotalAmount:   totalAmount,
		PromoID:       promoID,
		PromoName:     promoName,
		TxType:        1,
		TxStatus:      2,
		IdempotentKey: req.IdempotentKey,
		RequestID:     req.RequestID,
		ClientIP:      clientIP,
		StockBefore:   decrResult.StockBefore,
		StockAfter:    decrResult.StockAfter,
	}
	confirmAt := utils.NowUnix()
	tx.ConfirmAt = &confirmAt

	go func() {
		asyncCtx := utils.ContextWithTraceID(context.Background(), traceID)
		if err := s.transactionRepo.UpsertTransaction(asyncCtx, tx); err != nil {
			utils.SugarLogger.Errorw("async persist transaction failed",
				zap.String("trace_id", traceID),
				zap.String("transaction_id", txID),
				zap.Error(err),
			)
		}
	}()

	resp := &model.PickupResponse{
		TransactionID: txID,
		ShelfID:       req.ShelfID,
		ProductID:     req.ProductID,
		Quantity:      req.Quantity,
		StockBefore:   decrResult.StockBefore,
		StockAfter:    decrResult.StockAfter,
		UnitPrice:     unitPrice,
		OriginalPrice: originalPrice,
		TotalAmount:   totalAmount,
		DiscountAmt:   discountAmt,
		PromoID:       promoID,
		PromoName:     promoName,
		PickupAt:      utils.NowUnixMilli(),
		IsDuplicate:   false,
	}

	return &PickupResult{
		Response: resp,
		ErrCode:  utils.CodeSuccess,
	}
}

func reqFromDuplicateKey(ctx context.Context, repo *mysqlrepo.TransactionRepo, idempotentKey string, tx **model.Transaction) bool {
	existing, err := repo.GetByIdempotentKey(ctx, idempotentKey)
	if err != nil {
		return true
	}
	if existing != nil {
		*tx = existing
		return false
	}
	return true
}

func reqFromMysqlSlotCheck(ctx context.Context, repo *mysqlrepo.ShelfRepo, shelfID string, slotNo int, productID string) bool {
	sp, err := repo.GetShelfProduct(ctx, shelfID, slotNo)
	if err != nil {
		return true
	}
	if sp == nil {
		return true
	}
	return sp.ProductID == productID
}

func buildPickupResponseFromTx(tx *model.Transaction) *model.PickupResponse {
	discountAmt := int64(0)
	if tx.OriginalPrice > 0 && tx.OriginalPrice > tx.UnitPrice {
		discountAmt = (tx.OriginalPrice - tx.UnitPrice) * int64(tx.Quantity)
	}
	return &model.PickupResponse{
		TransactionID: tx.TransactionID,
		ShelfID:       tx.ShelfID,
		ProductID:     tx.ProductID,
		Quantity:      tx.Quantity,
		StockBefore:   tx.StockBefore,
		StockAfter:    tx.StockAfter,
		UnitPrice:     tx.UnitPrice,
		OriginalPrice: tx.OriginalPrice,
		TotalAmount:   tx.TotalAmount,
		DiscountAmt:   discountAmt,
		PromoID:       tx.PromoID,
		PromoName:     tx.PromoName,
		PickupAt:      tx.CreatedAt.UnixMilli(),
		IsDuplicate:   true,
	}
}

func (s *ShelfService) getProductWithCache(ctx context.Context, productID string) (*model.Product, error) {
	product, err := s.stockRepo.GetProductInfo(ctx, productID)
	if err != nil {
		return nil, err
	}
	if product != nil {
		return product, nil
	}

	product, err = s.shelfRepo.GetProductByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if product != nil {
		_ = s.stockRepo.SetProductInfo(ctx, product, 1*time.Hour)
	}
	return product, nil
}

type LockResult struct {
	Response *model.ShelfLockResponse
	ErrCode  int
	Err      error
}

func (s *ShelfService) LockShelf(ctx context.Context, req *model.ShelfLockRequest) *LockResult {
	traceID := utils.TraceIDFromContext(ctx)

	shelf, err := s.shelfRepo.GetShelfByID(ctx, req.ShelfID)
	if err != nil {
		utils.SugarLogger.Errorw("get shelf failed",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &LockResult{
			ErrCode: utils.CodeInternalError,
			Err:     errors.New("服务异常"),
		}
	}
	if shelf == nil {
		return &LockResult{
			ErrCode: utils.CodeNotFound,
			Err:     ErrShelfNotFound,
		}
	}
	if shelf.Status == model.ShelfStatusOffline {
		return &LockResult{
			ErrCode: utils.CodeShelfOffline,
			Err:     ErrShelfOffline,
		}
	}

	lockSeconds := req.LockSeconds
	if lockSeconds <= 0 {
		lockSeconds = s.cfg.Shelf.LockTimeoutSeconds
		if lockSeconds <= 0 {
			lockSeconds = 1800
		}
	}
	lockUntil := utils.NowUnix() + lockSeconds

	lockRecord := &model.ShelfLockRecord{
		ShelfID:    req.ShelfID,
		OperatorID: req.OperatorID,
		Operator:   req.OperatorName,
		LockType:   req.LockType,
		Reason:     req.Reason,
		LockUntil:  lockUntil,
		IsUnlock:   0,
	}

	lockResult, err := s.stockRepo.LockShelf(ctx, req.ShelfID, lockRecord, req.IdempotentKey, lockSeconds)
	if err != nil {
		utils.SugarLogger.Errorw("redis lock shelf failed",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &LockResult{
			ErrCode: utils.CodeServiceUnavailable,
			Err:     errors.New("锁定服务暂不可用"),
		}
	}

	if lockResult.IsDuplicate {
		activeLock, _ := s.shelfRepo.GetActiveLock(ctx, req.ShelfID)
		if activeLock != nil {
			return &LockResult{
				Response: &model.ShelfLockResponse{
					LockID:      activeLock.ID,
					ShelfID:     req.ShelfID,
					LockType:    activeLock.LockType,
					LockUntil:   activeLock.LockUntil,
					LockedAt:    activeLock.CreatedAt.Unix(),
					IsDuplicate: true,
				},
				ErrCode: utils.CodeSuccess,
			}
		}
		return &LockResult{
			Response: &model.ShelfLockResponse{
				ShelfID:     req.ShelfID,
				LockType:    req.LockType,
				LockUntil:   lockUntil,
				LockedAt:    utils.NowUnix(),
				IsDuplicate: true,
			},
			ErrCode: utils.CodeSuccess,
		}
	}

	if !lockResult.Success {
		return &LockResult{
			ErrCode: utils.CodeInternalError,
			Err:     errors.New("锁定失败"),
		}
	}

	go func() {
		asyncCtx := utils.ContextWithTraceID(context.Background(), traceID)
		if err := s.shelfRepo.CreateLockRecord(asyncCtx, lockRecord); err != nil {
			utils.SugarLogger.Errorw("async persist lock record failed",
				zap.String("trace_id", traceID),
				zap.String("shelf_id", req.ShelfID),
				zap.Error(err),
			)
		}
	}()

	return &LockResult{
		Response: &model.ShelfLockResponse{
			LockID:      lockRecord.ID,
			ShelfID:     req.ShelfID,
			LockType:    req.LockType,
			LockUntil:   lockUntil,
			LockedAt:    utils.NowUnix(),
			IsDuplicate: false,
		},
		ErrCode: utils.CodeSuccess,
	}
}

type UnlockResult struct {
	Response *model.ShelfUnlockResponse
	ErrCode  int
	Err      error
}

func (s *ShelfService) UnlockShelf(ctx context.Context, req *model.ShelfUnlockRequest) *UnlockResult {
	traceID := utils.TraceIDFromContext(ctx)

	shelf, err := s.shelfRepo.GetShelfByID(ctx, req.ShelfID)
	if err != nil {
		utils.SugarLogger.Errorw("get shelf failed",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &UnlockResult{
			ErrCode: utils.CodeInternalError,
			Err:     errors.New("服务异常"),
		}
	}
	if shelf == nil {
		return &UnlockResult{
			ErrCode: utils.CodeNotFound,
			Err:     ErrShelfNotFound,
		}
	}

	unlocked, err := s.stockRepo.UnlockShelf(ctx, req.ShelfID)
	if err != nil {
		utils.SugarLogger.Errorw("redis unlock shelf failed",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &UnlockResult{
			ErrCode: utils.CodeServiceUnavailable,
			Err:     errors.New("解锁服务暂不可用"),
		}
	}

	unlockAt := utils.NowUnix()
	go func() {
		asyncCtx := utils.ContextWithTraceID(context.Background(), traceID)
		rows, err := s.shelfRepo.UnlockShelf(asyncCtx, req.ShelfID, req.OperatorID, req.Reason, unlockAt)
		if err != nil {
			utils.SugarLogger.Errorw("async persist unlock record failed",
				zap.String("trace_id", traceID),
				zap.String("shelf_id", req.ShelfID),
				zap.Error(err),
			)
		}
		utils.SugarLogger.Infow("unlock shelf record updated",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Int64("rows_affected", rows),
		)
	}()

	_ = unlocked

	return &UnlockResult{
		Response: &model.ShelfUnlockResponse{
			ShelfID:    req.ShelfID,
			UnlockedAt: unlockAt,
		},
		ErrCode: utils.CodeSuccess,
	}
}

func (s *ShelfService) GetShelfStatus(ctx context.Context, shelfID string) (*model.ShelfStatusInfo, error) {
	info, err := s.stockRepo.GetShelfLockInfo(ctx, shelfID)
	if err != nil {
		return nil, err
	}

	if info.Status == 0 && !info.IsLocked {
		shelf, err := s.shelfRepo.GetShelfByID(ctx, shelfID)
		if err != nil {
			return nil, err
		}
		if shelf != nil {
			info.Status = shelf.Status
		}
	}

	return info, nil
}

func (s *ShelfService) GetStock(ctx context.Context, shelfID string, slotNo int) (*model.ShelfStockInfo, error) {
	sp, err := s.shelfRepo.GetShelfProduct(ctx, shelfID, slotNo)
	if err != nil {
		return nil, err
	}
	if sp == nil {
		return nil, ErrSlotMismatch
	}

	product, err := s.getProductWithCache(ctx, sp.ProductID)
	if err != nil {
		return nil, err
	}

	quantity, err := s.stockRepo.GetStock(ctx, shelfID, slotNo)
	if err != nil {
		return nil, err
	}

	etag, _ := s.stockRepo.GetETag(ctx, shelfID)

	info := &model.ShelfStockInfo{
		ShelfID:     shelfID,
		ProductID:   sp.ProductID,
		SlotNo:      slotNo,
		Quantity:    quantity,
		MaxCapacity: sp.MaxCapacity,
		ETagVersion: etag,
	}

	if product != nil {
		info.SKU = product.SKU
		info.ProductName = product.Name
		info.UnitPrice = product.Price
	}

	if sp.MaxCapacity > 0 {
		info.IsLowStock = int(float64(quantity)/float64(sp.MaxCapacity)*100) <= 20
	}
	info.IsSoldOut = quantity == 0

	return info, nil
}

type SelfCheckResult struct {
	ShelfID        string              `json:"shelf_id"`
	CheckedAt      int64               `json:"checked_at"`
	TotalSlots     int                 `json:"total_slots"`
	MismatchSlots  []map[string]string `json:"mismatch_slots"`
	NegativeStocks []map[string]string `json:"negative_stocks"`
	AnomalyCount   int                 `json:"anomaly_count"`
	IsHealthy      bool                `json:"is_healthy"`
}

func (s *ShelfService) SelfCheck(ctx context.Context, shelfID string, maxSlot int) (*SelfCheckResult, error) {
	traceID := utils.TraceIDFromContext(ctx)
	result := &SelfCheckResult{
		ShelfID:   shelfID,
		CheckedAt: utils.NowUnix(),
		IsHealthy: true,
	}

	if maxSlot <= 0 || maxSlot > 200 {
		maxSlot = 50
	}

	for sn := 1; sn <= maxSlot; sn++ {
		redisProduct, _ := s.stockRepo.GetShelfProduct(ctx, shelfID, sn)
		mysqlSP, _ := s.shelfRepo.GetShelfProduct(ctx, shelfID, sn)

		if mysqlSP != nil {
			result.TotalSlots++
			if redisProduct != "" && redisProduct != mysqlSP.ProductID {
				result.MismatchSlots = append(result.MismatchSlots, map[string]string{
					"slot":        fmt.Sprintf("%d", sn),
					"mysql_prod":  mysqlSP.ProductID,
					"redis_prod":  redisProduct,
				})
				result.AnomalyCount++
				result.IsHealthy = false
				utils.SugarLogger.Errorw("SELF-CHECK: PRODUCT MISMATCH",
					zap.String("trace_id", traceID),
					zap.String("shelf_id", shelfID),
					zap.Int("slot_no", sn),
					zap.String("mysql_prod", mysqlSP.ProductID),
					zap.String("redis_prod", redisProduct),
				)
			}

			stock, _ := s.stockRepo.GetStock(ctx, shelfID, sn)
			if stock < 0 {
				result.NegativeStocks = append(result.NegativeStocks, map[string]string{
					"slot":        fmt.Sprintf("%d", sn),
					"product_id":  mysqlSP.ProductID,
					"stock":       fmt.Sprintf("%d", stock),
				})
				result.AnomalyCount++
				result.IsHealthy = false
				utils.SugarLogger.Errorw("SELF-CHECK: NEGATIVE STOCK",
					zap.String("trace_id", traceID),
					zap.String("shelf_id", shelfID),
					zap.Int("slot_no", sn),
					zap.String("product_id", mysqlSP.ProductID),
					zap.Int("stock", stock),
				)
			}
		} else if redisProduct != "" {
			result.TotalSlots++
		}
	}

	return result, nil
}

func (s *ShelfService) GetAuditLogs(ctx context.Context, shelfID string, limit int64) ([]string, error) {
	return s.stockRepo.GetAuditLogs(ctx, shelfID, limit)
}

var (
	ErrPromoInvalidPrice     = errors.New("促销价格必须大于 0 且小于原价")
	ErrPromoInvalidTimeRange = errors.New("促销时间范围无效")
	ErrPromoSlotNotFound     = errors.New("货道不存在或商品不匹配")
)

type SetPromoResult struct {
	Response *model.SetPromoResponse
	ErrCode  int
	Err      error
}

func (s *ShelfService) SetPromo(ctx context.Context, req *model.SetPromoRequest) *SetPromoResult {
	traceID := utils.TraceIDFromContext(ctx)
	now := utils.NowUnix()

	if req.StartAt >= req.EndAt {
		return &SetPromoResult{ErrCode: utils.CodeBadRequest, Err: ErrPromoInvalidTimeRange}
	}
	if req.EndAt <= now {
		return &SetPromoResult{ErrCode: utils.CodeBadRequest, Err: ErrPromoInvalidTimeRange}
	}
	if req.PromoPrice <= 0 {
		return &SetPromoResult{ErrCode: utils.CodeBadRequest, Err: ErrPromoInvalidPrice}
	}

	sp, err := s.shelfRepo.GetShelfProduct(ctx, req.ShelfID, req.SlotNo)
	if err != nil {
		utils.SugarLogger.Errorw("get shelf product failed",
			zap.String("trace_id", traceID),
			zap.String("shelf_id", req.ShelfID),
			zap.Int("slot_no", req.SlotNo),
			zap.Error(err),
		)
		return &SetPromoResult{ErrCode: utils.CodeInternalError, Err: errors.New("服务异常")}
	}

	mysqlSlotProductID := ""
	if sp != nil {
		mysqlSlotProductID = sp.ProductID
	} else {
		redisProduct, redisErr := s.stockRepo.GetShelfProduct(ctx, req.ShelfID, req.SlotNo)
		if redisErr == nil && redisProduct != "" {
			mysqlSlotProductID = redisProduct
			utils.SugarLogger.Warnw("MySQL shelf_product not found, fallback to Redis mapping",
				zap.String("trace_id", traceID),
				zap.String("shelf_id", req.ShelfID),
				zap.Int("slot_no", req.SlotNo),
				zap.String("redis_product", redisProduct),
			)
		}
	}

	if mysqlSlotProductID == "" {
		return &SetPromoResult{ErrCode: utils.CodeNotFound, Err: ErrPromoSlotNotFound}
	}
	if mysqlSlotProductID != req.ProductID {
		return &SetPromoResult{
			ErrCode: utils.CodeSlotMismatch,
			Err:     fmt.Errorf("%w: 货道商品=%s, 请求商品=%s", ErrPromoSlotNotFound, mysqlSlotProductID, req.ProductID),
		}
	}

	product, err := s.getProductWithCache(ctx, req.ProductID)
	if err != nil {
		return &SetPromoResult{ErrCode: utils.CodeInternalError, Err: errors.New("服务异常")}
	}
	if product == nil {
		return &SetPromoResult{ErrCode: utils.CodeProductNotFound, Err: ErrProductNotFound}
	}
	if req.PromoPrice >= product.Price {
		return &SetPromoResult{
			ErrCode: utils.CodeBadRequest,
			Err:     fmt.Errorf("%w: 原价=%d分, 促销价=%d分", ErrPromoInvalidPrice, product.Price, req.PromoPrice),
		}
	}

	promo := &model.ShelfPromotion{
		PromoID:    req.PromoID,
		PromoName:  req.PromoName,
		ShelfID:    req.ShelfID,
		SlotNo:     req.SlotNo,
		ProductID:  req.ProductID,
		PromoPrice: req.PromoPrice,
		StartAt:    req.StartAt,
		EndAt:      req.EndAt,
		CreatedBy:  req.OperatorID,
		CreatedAt:  now,
	}

	if err := s.stockRepo.SetPromo(ctx, promo); err != nil {
		utils.SugarLogger.Errorw("set promo failed",
			zap.String("trace_id", traceID),
			zap.String("promo_id", req.PromoID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &SetPromoResult{
			ErrCode: utils.CodeServiceUnavailable,
			Err:     errors.New("促销服务暂不可用"),
		}
	}

	utils.SugarLogger.Infow("promo set successfully",
		zap.String("trace_id", traceID),
		zap.String("promo_id", req.PromoID),
		zap.String("promo_name", req.PromoName),
		zap.String("shelf_id", req.ShelfID),
		zap.Int("slot_no", req.SlotNo),
		zap.String("product_id", req.ProductID),
		zap.Int64("original_price", product.Price),
		zap.Int64("promo_price", req.PromoPrice),
		zap.Int64("start_at", req.StartAt),
		zap.Int64("end_at", req.EndAt),
		zap.String("operator", req.OperatorID),
	)

	return &SetPromoResult{
		Response: &model.SetPromoResponse{
			PromoID:    req.PromoID,
			ShelfID:    req.ShelfID,
			SlotNo:     req.SlotNo,
			ProductID:  req.ProductID,
			PromoPrice: req.PromoPrice,
			StartAt:    req.StartAt,
			EndAt:      req.EndAt,
			CreatedAt:  now,
			IsActive:   req.StartAt <= now && now <= req.EndAt,
		},
		ErrCode: utils.CodeSuccess,
	}
}

type CancelPromoResult struct {
	Response *model.CancelPromoResponse
	ErrCode  int
	Err      error
}

func (s *ShelfService) CancelPromo(ctx context.Context, req *model.CancelPromoRequest) *CancelPromoResult {
	traceID := utils.TraceIDFromContext(ctx)

	canceled, err := s.stockRepo.CancelPromo(ctx, req.ShelfID, req.SlotNo, req.PromoID)
	if err != nil {
		utils.SugarLogger.Errorw("cancel promo failed",
			zap.String("trace_id", traceID),
			zap.String("promo_id", req.PromoID),
			zap.String("shelf_id", req.ShelfID),
			zap.Error(err),
		)
		return &CancelPromoResult{
			ErrCode: utils.CodeServiceUnavailable,
			Err:     errors.New("促销服务暂不可用"),
		}
	}

	utils.SugarLogger.Infow("promo canceled",
		zap.String("trace_id", traceID),
		zap.String("promo_id", req.PromoID),
		zap.String("shelf_id", req.ShelfID),
		zap.Int("slot_no", req.SlotNo),
		zap.Bool("canceled", canceled),
		zap.String("operator", req.OperatorID),
	)

	return &CancelPromoResult{
		Response: &model.CancelPromoResponse{
			PromoID:    req.PromoID,
			ShelfID:    req.ShelfID,
			SlotNo:     req.SlotNo,
			Canceled:   canceled,
			CanceledAt: utils.NowUnix(),
		},
		ErrCode: utils.CodeSuccess,
	}
}

func (s *ShelfService) GetPromo(ctx context.Context, shelfID string, slotNo int) (*redisrepo.PromoGetResult, error) {
	return s.stockRepo.GetPromo(ctx, shelfID, slotNo)
}
