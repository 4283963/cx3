package mysql

import (
	"context"
	"cx3/model"
	"cx3/utils"
	"errors"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TransactionRepo struct {
	db *gorm.DB
}

func NewTransactionRepo(db *gorm.DB) *TransactionRepo {
	return &TransactionRepo{db: db}
}

func (r *TransactionRepo) CreateTransaction(ctx context.Context, tx *model.Transaction) error {
	if r.db == nil {
		return nil
	}
	err := r.db.WithContext(ctx).Create(tx).Error
	if err != nil {
		utils.SugarLogger.Errorw("create transaction failed",
			zap.String("trace_id", utils.TraceIDFromContext(ctx)),
			zap.String("transaction_id", tx.TransactionID),
			zap.String("shelf_id", tx.ShelfID),
			zap.Error(err),
		)
	}
	return err
}

func (r *TransactionRepo) CreateTransactionAsync(ctx context.Context, tx *model.Transaction) error {
	if r.db == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			done <- ctx.Err()
			return
		default:
		}
		done <- r.db.WithContext(ctx).Create(tx).Error
	}()

	select {
	case err := <-done:
		if err != nil {
			utils.SugarLogger.Errorw("async create transaction failed",
				zap.String("trace_id", utils.TraceIDFromContext(ctx)),
				zap.String("transaction_id", tx.TransactionID),
				zap.Error(err),
			)
		}
		return err
	case <-ctx.Done():
		utils.SugarLogger.Warnw("async create transaction timeout",
			zap.String("trace_id", utils.TraceIDFromContext(ctx)),
			zap.String("transaction_id", tx.TransactionID),
		)
		return ctx.Err()
	}
}

func (r *TransactionRepo) UpsertTransaction(ctx context.Context, tx *model.Transaction) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "idempotent_key"}},
		DoNothing: true,
	}).Create(tx).Error
}

func (r *TransactionRepo) GetByIdempotentKey(ctx context.Context, idempotentKey string) (*model.Transaction, error) {
	if r.db == nil {
		return nil, nil
	}
	var tx model.Transaction
	err := r.db.WithContext(ctx).Where("idempotent_key = ?", idempotentKey).First(&tx).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &tx, nil
}

func (r *TransactionRepo) GetByTransactionID(ctx context.Context, transactionID string) (*model.Transaction, error) {
	if r.db == nil {
		return nil, nil
	}
	var tx model.Transaction
	err := r.db.WithContext(ctx).Where("transaction_id = ?", transactionID).First(&tx).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &tx, nil
}

func (r *TransactionRepo) ListByShelfID(ctx context.Context, shelfID string, limit, offset int) ([]model.Transaction, int64, error) {
	if r.db == nil {
		return nil, 0, nil
	}
	var list []model.Transaction
	var total int64

	db := r.db.WithContext(ctx).Model(&model.Transaction{}).Where("shelf_id = ?", shelfID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, 0, err
	}

	return list, total, nil
}

func (r *TransactionRepo) UpdateStatus(ctx context.Context, id int64, status int, confirmAt *int64) error {
	if r.db == nil {
		return nil
	}
	updates := map[string]interface{}{
		"tx_status": status,
	}
	if confirmAt != nil {
		updates["confirm_at"] = *confirmAt
	}
	return r.db.WithContext(ctx).Model(&model.Transaction{}).
		Where("id = ?", id).
		Updates(updates).Error
}
