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
		UnitPrice:     product.Price,
		TotalAmount:   product.Price * int64(req.Quantity),
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
		UnitPrice:     product.Price,
		TotalAmount:   product.Price * int64(req.Quantity),
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
	return &model.PickupResponse{
		TransactionID: tx.TransactionID,
		ShelfID:       tx.ShelfID,
		ProductID:     tx.ProductID,
		Quantity:      tx.Quantity,
		StockBefore:   tx.StockBefore,
		StockAfter:    tx.StockAfter,
		UnitPrice:     tx.UnitPrice,
		TotalAmount:   tx.TotalAmount,
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
