package redis

import (
	"context"
	"cx3/config"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var Client *redis.Client

func InitRedis(cfg *config.RedisConfig) error {
	Client = redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		MaxRetries:   cfg.MaxRetries,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	return nil
}

func CloseRedis() {
	if Client != nil {
		_ = Client.Close()
	}
}

var (
	ErrStockNotEnough   = errors.New("stock not enough")
	ErrShelfLocked      = errors.New("shelf is locked")
	ErrShelfNotExist    = errors.New("shelf not exist")
	ErrSlotMismatch     = errors.New("slot product mismatch")
	ErrIdempotentExists = errors.New("idempotent key exists")
)

const (
	decrStockLua = `
local stock_key = KEYS[1]
local lock_key = KEYS[2]
local product_key = KEYS[3]
local idempotent_key = KEYS[4]
local quantity = tonumber(ARGV[1])
local expected_product = ARGV[2]
local idempotent_val = ARGV[3]
local ttl_seconds = tonumber(ARGV[4])

if redis.call('EXISTS', idempotent_key) == 1 then
    local prev_result = redis.call('GET', idempotent_key)
    return '-2:' .. prev_result
end

local is_locked = redis.call('EXISTS', lock_key)
if is_locked == 1 then
    local lock_info = redis.call('GET', lock_key)
    return '-3:' .. (lock_info or '')
end

if expected_product ~= '' then
    local current_product = redis.call('GET', product_key)
    if current_product ~= expected_product then
        return '-4:' .. (current_product or '')
    end
end

local current_stock = tonumber(redis.call('GET', stock_key) or '0')
if current_stock < quantity then
    return '-1:' .. tostring(current_stock)
end

local new_stock = current_stock - quantity
redis.call('SET', stock_key, new_stock)
redis.call('SETEX', idempotent_key, ttl_seconds, tostring(current_stock) .. ':' .. tostring(new_stock))

return '1:' .. tostring(current_stock) .. ':' .. tostring(new_stock)
`

	lockShelfLua = `
local lock_key = KEYS[1]
local idempotent_key = KEYS[2]
local lock_value = ARGV[1]
local lock_ttl = tonumber(ARGV[2])
local idempotent_ttl = tonumber(ARGV[3])
local idempotent_val = ARGV[4]

if redis.call('EXISTS', idempotent_key) == 1 then
    local prev = redis.call('GET', idempotent_key)
    return '-1:' .. prev
end

redis.call('SET', lock_key, lock_value, 'EX', lock_ttl)
redis.call('SETEX', idempotent_key, idempotent_ttl, idempotent_val)

return '1:OK'
`

	unlockShelfLua = `
local lock_key = KEYS[1]
if redis.call('EXISTS', lock_key) == 1 then
    redis.call('DEL', lock_key)
    return '1:UNLOCKED'
end
return '0:NOT_LOCKED'
`

	getStockBatchLua = `
local result = {}
for i = 1, #KEYS do
    local stock = tonumber(redis.call('GET', KEYS[i]) or '0')
    local product_key = string.gsub(KEYS[i], 'shelf:stock:', 'shelf:product:')
    local product = redis.call('GET', product_key) or ''
    table.insert(result, stock .. '|' .. product)
end
return table.concat(result, ',')
`
)

var (
	decrStockScript    *redis.Script
	lockShelfScript    *redis.Script
	unlockShelfScript  *redis.Script
	getStockBatchScript *redis.Script
)

func init() {
	decrStockScript = redis.NewScript(decrStockLua)
	lockShelfScript = redis.NewScript(lockShelfLua)
	unlockShelfScript = redis.NewScript(unlockShelfLua)
	getStockBatchScript = redis.NewScript(getStockBatchLua)
}
