package mysql

import (
	"context"
	"cx3/model"
	"cx3/utils"
	"errors"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ShelfRepo struct {
	db *gorm.DB
}

func NewShelfRepo(db *gorm.DB) *ShelfRepo {
	return &ShelfRepo{db: db}
}

func (r *ShelfRepo) CreateLockRecord(ctx context.Context, record *model.ShelfLockRecord) error {
	if r.db == nil {
		return nil
	}
	err := r.db.WithContext(ctx).Create(record).Error
	if err != nil {
		utils.SugarLogger.Errorw("create shelf lock record failed",
			zap.String("trace_id", utils.TraceIDFromContext(ctx)),
			zap.String("shelf_id", record.ShelfID),
			zap.Error(err),
		)
	}
	return err
}

func (r *ShelfRepo) UnlockShelf(ctx context.Context, shelfID string, unlockBy string, unlockReason string, unlockAt int64) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Model(&model.ShelfLockRecord{}).
		Where("shelf_id = ? AND is_unlock = 0 AND lock_until > ?", shelfID, utils.NowUnix()).
		Updates(map[string]interface{}{
			"is_unlock":     1,
			"unlock_by":     unlockBy,
			"unlock_at":     unlockAt,
			"unlock_reason": unlockReason,
		})

	return result.RowsAffected, result.Error
}

func (r *ShelfRepo) GetActiveLock(ctx context.Context, shelfID string) (*model.ShelfLockRecord, error) {
	if r.db == nil {
		return nil, nil
	}
	var record model.ShelfLockRecord
	err := r.db.WithContext(ctx).
		Where("shelf_id = ? AND is_unlock = 0 AND lock_until > ?", shelfID, utils.NowUnix()).
		Order("id DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (r *ShelfRepo) GetByIdempotentKey(ctx context.Context, idempotentKey string) (*model.ShelfLockRecord, error) {
	if r.db == nil {
		return nil, nil
	}
	var record model.ShelfLockRecord
	err := r.db.WithContext(ctx).
		Where("CONCAT(operator_id, ':', shelf_id, ':', lock_until) = ?", idempotentKey).
		Order("id DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (r *ShelfRepo) GetShelfByID(ctx context.Context, shelfID string) (*model.Shelf, error) {
	if r.db == nil {
		return &model.Shelf{ShelfID: shelfID, Status: 0}, nil
	}
	var shelf model.Shelf
	err := r.db.WithContext(ctx).Where("shelf_id = ?", shelfID).First(&shelf).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &model.Shelf{ShelfID: shelfID, Status: 0}, nil
		}
		return nil, err
	}
	return &shelf, nil
}

func (r *ShelfRepo) GetShelfProduct(ctx context.Context, shelfID string, slotNo int) (*model.ShelfProduct, error) {
	if r.db == nil {
		return nil, nil
	}
	var sp model.ShelfProduct
	err := r.db.WithContext(ctx).
		Where("shelf_id = ? AND slot_no = ?", shelfID, slotNo).
		First(&sp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &sp, nil
}

func (r *ShelfRepo) GetProductByID(ctx context.Context, productID string) (*model.Product, error) {
	if r.db == nil {
		return nil, nil
	}
	var product model.Product
	err := r.db.WithContext(ctx).Where("product_id = ?", productID).First(&product).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &product, nil
}

func (r *ShelfRepo) UpdateShelfStatus(ctx context.Context, shelfID string, status model.ShelfStatus) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Shelf{}).
		Where("shelf_id = ?", shelfID).
		Update("status", status).Error
}

func (r *ShelfRepo) ListShelfProducts(ctx context.Context, shelfID string) ([]model.ShelfProduct, error) {
	if r.db == nil {
		return nil, nil
	}
	var list []model.ShelfProduct
	err := r.db.WithContext(ctx).
		Where("shelf_id = ?", shelfID).
		Order("sort_order ASC, slot_no ASC").
		Find(&list).Error
	return list, err
}
