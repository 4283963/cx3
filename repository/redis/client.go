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
local audit_key = KEYS[5]
local quantity = tonumber(ARGV[1])
local expected_product = ARGV[2]
local idempotent_val = ARGV[3]
local ttl_seconds = tonumber(ARGV[4])
local trace_id = ARGV[5]
local ts = ARGV[6]

local result_code = '0'
local result_payload = ''

local function append_audit(status, detail)
    local log_entry = table.concat({
        'ts=', ts,
        ',trace=', trace_id,
        ',idem=', idempotent_val,
        ',qty=', tostring(quantity),
        ',exp=', expected_product,
        ',cur=', (redis.call('GET', product_key) or ''),
        ',stk_bef=', (redis.call('GET', stock_key) or '0'),
        ',stk_aft=', '',
        ',status=', status,
        ',detail=', detail
    }, '')
    redis.call('RPUSH', audit_key, log_entry)
    redis.call('LTRIM', audit_key, -50000, -1)
end

if redis.call('EXISTS', idempotent_key) == 1 then
    local prev_result = redis.call('GET', idempotent_key)
    append_audit('DUP', prev_result)
    return '-2:' .. prev_result
end

local is_locked = redis.call('EXISTS', lock_key)
if is_locked == 1 then
    local lock_info = redis.call('GET', lock_key)
    append_audit('LOCKED', string.sub(lock_info or '', 1, 80))
    return '-3:' .. (lock_info or '')
end

if expected_product ~= '' then
    local current_product = redis.call('GET', product_key)
    if current_product ~= expected_product then
        append_audit('MISMATCH', 'got=' .. (current_product or 'nil') .. ';want=' .. expected_product)
        return '-4:' .. (current_product or '')
    end
end

local current_stock = tonumber(redis.call('GET', stock_key) or '0')
if current_stock < quantity then
    append_audit('NOSTOCK', 'have=' .. tostring(current_stock) .. ';need=' .. tostring(quantity))
    return '-1:' .. tostring(current_stock)
end

local new_stock = current_stock - quantity
redis.call('SET', stock_key, new_stock)
local idem_payload = tostring(current_stock) .. ':' .. tostring(new_stock)
redis.call('SETEX', idempotent_key, ttl_seconds, idem_payload)

append_audit('OK', 'deducted=' .. tostring(quantity))
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

	getPromoLua = `
local promo_key = KEYS[1]
local now_ts = tonumber(ARGV[1])

if redis.call('EXISTS', promo_key) == 0 then
    return '0:'
end

local raw = redis.call('GET', promo_key)
if not raw or raw == '' then
    return '0:'
end

local promo_id, promo_name, product_id, promo_price, start_at, end_at, created_by = raw:match(
    '([^|]+)|([^|]+)|([^|]+)|([^|]+)|([^|]+)|([^|]+)|([^|]+)'
)
if not promo_id then
    return '0:'
end

local start_ts = tonumber(start_at)
local end_ts = tonumber(end_at)

if now_ts < start_ts then
    return '-1:' .. raw
end

if now_ts > end_ts then
    redis.call('DEL', promo_key)
    return '-2:'
end

return '1:' .. raw
`

	cancelPromoLua = `
local promo_key = KEYS[1]
local expected_promo_id = ARGV[1]

if redis.call('EXISTS', promo_key) == 0 then
    return '0:NOT_FOUND'
end

local raw = redis.call('GET', promo_key)
local promo_id = raw:match('([^|]+)')

if expected_promo_id ~= '' and promo_id ~= expected_promo_id then
    return '-1:MISMATCH:' .. (promo_id or '')
end

redis.call('DEL', promo_key)
return '1:CANCELED'
`
)

var (
	decrStockScript     *redis.Script
	lockShelfScript     *redis.Script
	unlockShelfScript   *redis.Script
	getStockBatchScript *redis.Script
	getPromoScript      *redis.Script
	cancelPromoScript   *redis.Script
)

func init() {
	decrStockScript = redis.NewScript(decrStockLua)
	lockShelfScript = redis.NewScript(lockShelfLua)
	unlockShelfScript = redis.NewScript(unlockShelfLua)
	getStockBatchScript = redis.NewScript(getStockBatchLua)
	getPromoScript = redis.NewScript(getPromoLua)
	cancelPromoScript = redis.NewScript(cancelPromoLua)
}
