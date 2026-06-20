package redis

import (
	"context"
	"cx3/config"
	"cx3/model"
	"cx3/utils"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTest(t *testing.T) (*StockRepo, *redis.Client, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := &config.Config{
		Idempotent: config.IdempotentConfig{TTLSeconds: 300},
		Shelf:      config.ShelfConfig{LockTimeoutSeconds: 1800},
	}
	repo := NewStockRepo(client, cfg)
	return repo, client, mr
}

func TestDecrStock_Success(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-001"
	slotNo := 1
	productID := "P-001"
	idemKey := "test-idem-001"
	initialStock := 50

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), initialStock, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)

	result, err := repo.DecrStock(ctx, shelfID, slotNo, productID, 5, idemKey)
	require.NoError(t, err)
	assert.True(t, result.Success, "Decr should succeed")
	assert.False(t, result.IsDuplicate)
	assert.Equal(t, initialStock, result.StockBefore)
	assert.Equal(t, initialStock-5, result.StockAfter)

	finalStock, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
	assert.Equal(t, initialStock-5, finalStock)
}

func TestDecrStock_NotEnough(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-002"
	slotNo := 2
	productID := "P-002"
	idemKey := "test-idem-002"

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 3, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)

	result, err := repo.DecrStock(ctx, shelfID, slotNo, productID, 10, idemKey)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Equal(t, -1, result.ErrCode)
}

func TestDecrStock_Idempotent(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-003"
	slotNo := 3
	productID := "P-003"
	idemKey := "test-idem-003"
	initialStock := 20

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), initialStock, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)

	r1, err := repo.DecrStock(ctx, shelfID, slotNo, productID, 2, idemKey)
	require.NoError(t, err)
	assert.True(t, r1.Success)
	assert.False(t, r1.IsDuplicate)

	r2, err := repo.DecrStock(ctx, shelfID, slotNo, productID, 2, idemKey)
	require.NoError(t, err)
	assert.True(t, r2.IsDuplicate, "Should detect duplicate idempotent key")
	assert.Equal(t, r1.StockBefore, r2.StockBefore)
	assert.Equal(t, r1.StockAfter, r2.StockAfter)

	finalStock, _ := client.Get(ctx, utils.BuildShelfStockKey(shelfID, slotNo)).Int()
	assert.Equal(t, initialStock-2, finalStock, "Stock should only be deducted once")
}

func TestDecrStock_ShelfLocked(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-004"
	slotNo := 4
	productID := "P-004"
	idemKey := "test-idem-004"

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 100, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), productID, 0)

	lockInfo := &model.ShelfLockRecord{
		ShelfID: shelfID, LockType: 1, Reason: "test lock", LockUntil: utils.NowUnix() + 3600,
	}
	lockJSON, _ := json.Marshal(lockInfo)
	client.Set(ctx, utils.BuildShelfLockKey(shelfID), lockJSON, time.Hour)

	result, err := repo.DecrStock(ctx, shelfID, slotNo, productID, 1, idemKey)
	require.NoError(t, err)
	assert.Equal(t, -3, result.ErrCode, "Should return locked error code")
	assert.Contains(t, result.LockInfo, "test lock")
}

func TestDecrStock_SlotMismatch(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-005"
	slotNo := 5
	actualProduct := "P-REAL"
	requestedProduct := "P-REQUESTED"
	idemKey := "test-idem-005"

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 50, 0)
	client.Set(ctx, utils.BuildShelfProductKey(shelfID, slotNo), actualProduct, 0)

	result, err := repo.DecrStock(ctx, shelfID, slotNo, requestedProduct, 1, idemKey)
	require.NoError(t, err)
	assert.Equal(t, -4, result.ErrCode, "Should return slot mismatch error")
}

func TestLockShelf_Success(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-LOCK-001"
	idemKey := "lock-idem-001"
	lockSeconds := int64(1800)

	lockInfo := &model.ShelfLockRecord{
		ShelfID:    shelfID,
		OperatorID: "OP-001",
		Operator:   "Admin",
		LockType:   1,
		Reason:     "重力异常",
		LockUntil:  utils.NowUnix() + lockSeconds,
	}

	result, err := repo.LockShelf(ctx, shelfID, lockInfo, idemKey, lockSeconds)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.False(t, result.IsDuplicate)

	exists := client.Exists(ctx, utils.BuildShelfLockKey(shelfID)).Val()
	assert.Equal(t, int64(1), exists, "Lock key should exist")

	ttl := client.TTL(ctx, utils.BuildShelfLockKey(shelfID)).Val()
	assert.Greater(t, ttl, 0*time.Second)
	assert.LessOrEqual(t, ttl, time.Duration(lockSeconds)*time.Second)
}

func TestUnlockShelf(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-UNLOCK-001"

	client.Set(ctx, utils.BuildShelfLockKey(shelfID), "locked", time.Hour)

	unlocked, err := repo.UnlockShelf(ctx, shelfID)
	require.NoError(t, err)
	assert.True(t, unlocked)

	exists := client.Exists(ctx, utils.BuildShelfLockKey(shelfID)).Val()
	assert.Equal(t, int64(0), exists, "Lock key should be removed")

	unlocked2, err := repo.UnlockShelf(ctx, shelfID)
	require.NoError(t, err)
	assert.False(t, unlocked2, "Should return false when not locked")
}

func TestGetStock(t *testing.T) {
	repo, client, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-GET-001"
	slotNo := 1

	quantity, err := repo.GetStock(ctx, shelfID, slotNo)
	require.NoError(t, err)
	assert.Equal(t, 0, quantity, "Non-existent key should return 0")

	client.Set(ctx, utils.BuildShelfStockKey(shelfID, slotNo), 42, 0)
	quantity, err = repo.GetStock(ctx, shelfID, slotNo)
	require.NoError(t, err)
	assert.Equal(t, 42, quantity)
}

func TestETag(t *testing.T) {
	repo, _, mr := setupTest(t)
	defer mr.Close()

	ctx := context.Background()
	shelfID := "SH-ETAG-001"

	etag, err := repo.GetETag(ctx, shelfID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), etag)

	newETag, err := repo.IncrETag(ctx, shelfID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), newETag)

	newETag, err = repo.IncrETag(ctx, shelfID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), newETag)

	current, _ := repo.GetETag(ctx, shelfID)
	assert.Equal(t, newETag, current)
}

func BenchmarkDecrStock(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := &config.Config{
		Idempotent: config.IdempotentConfig{TTLSeconds: 300},
	}
	repo := NewStockRepo(client, cfg)
	ctx := context.Background()
	shelfID := "SH-BENCH"
	productID := "P-BENCH"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			shelfKey := fmt.Sprintf("%s-%d", shelfID, i%100)
			slot := (i % 10) + 1
			stockKey := utils.BuildShelfStockKey(shelfKey, slot)
			productKey := utils.BuildShelfProductKey(shelfKey, slot)
			client.Set(ctx, stockKey, 1000, 0)
			client.Set(ctx, productKey, productID, 0)

			idemKey := fmt.Sprintf("bench-idem-%d-%d", i, time.Now().UnixNano())
			_, _ = repo.DecrStock(ctx, shelfKey, slot, productID, 1, idemKey)
			i++
		}
	})
}
