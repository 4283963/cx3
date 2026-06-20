package redis

import (
	"context"
	"cx3/config"
	"cx3/model"
	"cx3/utils"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type StockRepo struct {
	client *redis.Client
	cfg    *config.Config
}

func NewStockRepo(client *redis.Client, cfg *config.Config) *StockRepo {
	return &StockRepo{
		client: client,
		cfg:    cfg,
	}
}

type DecrStockResult struct {
	Success       bool
	IsDuplicate   bool
	StockBefore   int
	StockAfter    int
	PrevTxResult  string
	LockInfo      string
	ErrCode       int
}

type LockShelfResult struct {
	Success     bool
	IsDuplicate bool
	PrevResult  string
}

func (r *StockRepo) DecrStock(ctx context.Context, shelfID string, slotNo int, productID string,
	quantity int, idempotentKey string) (*DecrStockResult, error) {

	stockKey := utils.BuildShelfStockKey(shelfID, slotNo)
	lockKey := utils.BuildShelfLockKey(shelfID)
	productKey := utils.BuildShelfProductKey(shelfID, slotNo)
	idemKey := utils.BuildIdempotentKey(idempotentKey)

	ttl := r.cfg.Idempotent.TTLSeconds
	if ttl <= 0 {
		ttl = 300
	}

	result, err := decrStockScript.Run(ctx, r.client,
		[]string{stockKey, lockKey, productKey, idemKey},
		quantity, productID, idempotentKey, ttl,
	).Text()

	if err != nil {
		utils.SugarLogger.Errorw("decr stock lua script failed",
			zap.String("trace_id", utils.TraceIDFromContext(ctx)),
			zap.String("shelf_id", shelfID),
			zap.Int("slot_no", slotNo),
			zap.String("product_id", productID),
			zap.Error(err),
		)
		return nil, err
	}

	return parseDecrStockResult(result)
}

func parseDecrStockResult(result string) (*DecrStockResult, error) {
	idx := strings.Index(result, ":")
	if idx < 0 {
		return nil, fmt.Errorf("invalid decr stock result format: %s", result)
	}

	codeStr := result[:idx]
	rest := result[idx+1:]

	code, err := strconv.Atoi(codeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid result code: %s", codeStr)
	}

	res := &DecrStockResult{
		ErrCode: code,
	}

	switch code {
	case 1:
		res.Success = true
		stocks := strings.SplitN(rest, ":", 2)
		if len(stocks) == 2 {
			before, e1 := strconv.Atoi(stocks[0])
			after, e2 := strconv.Atoi(stocks[1])
			if e1 == nil && e2 == nil {
				res.StockBefore = before
				res.StockAfter = after
			}
		}
	case -1:
		res.PrevTxResult = rest
	case -2:
		res.IsDuplicate = true
		res.PrevTxResult = rest
		stocks := strings.SplitN(rest, ":", 2)
		if len(stocks) == 2 {
			before, e1 := strconv.Atoi(stocks[0])
			after, e2 := strconv.Atoi(stocks[1])
			if e1 == nil && e2 == nil {
				res.StockBefore = before
				res.StockAfter = after
			}
		}
	case -3:
		res.LockInfo = rest
	case -4:
	}

	return res, nil
}

func (r *StockRepo) LockShelf(ctx context.Context, shelfID string, lockInfo *model.ShelfLockRecord,
	idempotentKey string, lockSeconds int64) (*LockShelfResult, error) {

	lockKey := utils.BuildShelfLockKey(shelfID)
	idemKey := utils.BuildIdempotentKey(idempotentKey)

	infoBytes, _ := json.Marshal(lockInfo)
	lockValue := string(infoBytes)
	idempotentTTL := r.cfg.Idempotent.TTLSeconds
	if idempotentTTL <= 0 {
		idempotentTTL = 300
	}

	result, err := lockShelfScript.Run(ctx, r.client,
		[]string{lockKey, idemKey},
		lockValue, lockSeconds, idempotentTTL, "locked:"+strconv.FormatInt(lockInfo.LockUntil, 10),
	).Text()

	if err != nil {
		utils.SugarLogger.Errorw("lock shelf lua script failed",
			zap.String("trace_id", utils.TraceIDFromContext(ctx)),
			zap.String("shelf_id", shelfID),
			zap.Error(err),
		)
		return nil, err
	}

	return parseLockResult(result)
}

func parseLockResult(result string) (*LockShelfResult, error) {
	parts := strings.SplitN(result, ":", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid lock result: %s", result)
	}

	code, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid result code: %s", parts[0])
	}

	res := &LockShelfResult{}
	switch code {
	case 1:
		res.Success = true
	case -1:
		res.IsDuplicate = true
		if len(parts) >= 2 {
			res.PrevResult = parts[1]
		}
	}

	return res, nil
}

func (r *StockRepo) UnlockShelf(ctx context.Context, shelfID string) (bool, error) {
	lockKey := utils.BuildShelfLockKey(shelfID)

	result, err := unlockShelfScript.Run(ctx, r.client,
		[]string{lockKey},
	).Text()

	if err != nil {
		utils.SugarLogger.Errorw("unlock shelf lua script failed",
			zap.String("trace_id", utils.TraceIDFromContext(ctx)),
			zap.String("shelf_id", shelfID),
			zap.Error(err),
		)
		return false, err
	}

	parts := strings.SplitN(result, ":", 2)
	if len(parts) < 2 {
		return false, nil
	}

	code, _ := strconv.Atoi(parts[0])
	return code == 1, nil
}

func (r *StockRepo) GetStock(ctx context.Context, shelfID string, slotNo int) (int, error) {
	stockKey := utils.BuildShelfStockKey(shelfID, slotNo)
	val, err := r.client.Get(ctx, stockKey).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

func (r *StockRepo) SetStock(ctx context.Context, shelfID string, slotNo int, quantity int) error {
	stockKey := utils.BuildShelfStockKey(shelfID, slotNo)
	return r.client.Set(ctx, stockKey, quantity, 0).Err()
}

func (r *StockRepo) GetShelfProduct(ctx context.Context, shelfID string, slotNo int) (string, error) {
	productKey := utils.BuildShelfProductKey(shelfID, slotNo)
	val, err := r.client.Get(ctx, productKey).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (r *StockRepo) SetShelfProduct(ctx context.Context, shelfID string, slotNo int, productID string) error {
	productKey := utils.BuildShelfProductKey(shelfID, slotNo)
	return r.client.Set(ctx, productKey, productID, 0).Err()
}

func (r *StockRepo) GetShelfLockInfo(ctx context.Context, shelfID string) (*model.ShelfStatusInfo, error) {
	lockKey := utils.BuildShelfLockKey(shelfID)
	statusKey := utils.BuildShelfStatusKey(shelfID)

	info := &model.ShelfStatusInfo{
		ShelfID: shelfID,
	}

	lockVal, err := r.client.Get(ctx, lockKey).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	if err == nil && lockVal != "" {
		info.IsLocked = true
		var lockRecord model.ShelfLockRecord
		if json.Unmarshal([]byte(lockVal), &lockRecord) == nil {
			info.LockType = lockRecord.LockType
			info.LockUntil = lockRecord.LockUntil
			info.Reason = lockRecord.Reason
		}
	}

	statusVal, err := r.client.Get(ctx, statusKey).Int()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	info.Status = model.ShelfStatus(statusVal)

	if info.IsLocked {
		info.Status = model.ShelfStatusLocked
	}

	return info, nil
}

func (r *StockRepo) GetProductInfo(ctx context.Context, productID string) (*model.Product, error) {
	productKey := utils.BuildProductInfoKey(productID)
	val, err := r.client.Get(ctx, productKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var product model.Product
	if err := json.Unmarshal([]byte(val), &product); err != nil {
		return nil, err
	}
	return &product, nil
}

func (r *StockRepo) SetProductInfo(ctx context.Context, product *model.Product, ttl time.Duration) error {
	productKey := utils.BuildProductInfoKey(product.ProductID)
	data, err := json.Marshal(product)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, productKey, data, ttl).Err()
}

func (r *StockRepo) IncrETag(ctx context.Context, shelfID string) (int64, error) {
	etagKey := utils.BuildETagKey(shelfID)
	return r.client.Incr(ctx, etagKey).Result()
}

func (r *StockRepo) GetETag(ctx context.Context, shelfID string) (int64, error) {
	etagKey := utils.BuildETagKey(shelfID)
	val, err := r.client.Get(ctx, etagKey).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}
