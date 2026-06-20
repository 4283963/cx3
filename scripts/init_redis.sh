#!/bin/bash

REDIS_CLI=${REDIS_CLI:-redis-cli}
REDIS_HOST=${REDIS_HOST:-127.0.0.1}
REDIS_PORT=${REDIS_PORT:-6379}
REDIS_PASSWORD=${REDIS_PASSWORD:-}
REDIS_DB=${REDIS_DB:-0}

AUTH_ARGS=""
if [ -n "$REDIS_PASSWORD" ]; then
    AUTH_ARGS="-a $REDIS_PASSWORD"
fi

REDIS_CMD="$REDIS_CLI -h $REDIS_HOST -p $REDIS_PORT $AUTH_ARGS -n $REDIS_DB"

echo "Initializing shelf stock data (with HashTag for Cluster)..."

$REDIS_CMD SET "shelf:stock:{SH001}:1" 25
$REDIS_CMD SET "shelf:stock:{SH001}:2" 18
$REDIS_CMD SET "shelf:stock:{SH001}:3" 30
$REDIS_CMD SET "shelf:stock:{SH001}:4" 12
$REDIS_CMD SET "shelf:stock:{SH001}:5" 20

$REDIS_CMD SET "shelf:stock:{SH002}:1" 28
$REDIS_CMD SET "shelf:stock:{SH002}:2" 15

$REDIS_CMD SET "shelf:stock:{SH003}:1" 22

echo "Initializing shelf product mapping..."

$REDIS_CMD SET "shelf:product:{SH001}:1" "P001"
$REDIS_CMD SET "shelf:product:{SH001}:2" "P002"
$REDIS_CMD SET "shelf:product:{SH001}:3" "P003"
$REDIS_CMD SET "shelf:product:{SH001}:4" "P004"
$REDIS_CMD SET "shelf:product:{SH001}:5" "P005"

$REDIS_CMD SET "shelf:product:{SH002}:1" "P001"
$REDIS_CMD SET "shelf:product:{SH002}:2" "P003"

$REDIS_CMD SET "shelf:product:{SH003}:1" "P002"

echo "Initializing shelf status..."

$REDIS_CMD SET "shelf:status:{SH001}" 0
$REDIS_CMD SET "shelf:status:{SH002}" 0
$REDIS_CMD SET "shelf:status:{SH003}" 0

echo "Initializing ETag versions..."

$REDIS_CMD SET "shelf:etag:{SH001}" 1
$REDIS_CMD SET "shelf:etag:{SH002}" 1
$REDIS_CMD SET "shelf:etag:{SH003}" 1

echo "Initializing product cache..."

$REDIS_CMD SETEX "product:info:{P001}" 3600 '{"product_id":"P001","sku":"SKU0001","barcode":"6901234567890","name":"农夫山泉550ml","category_id":"CAT001","price":200,"cost":100,"is_active":1}'
$REDIS_CMD SETEX "product:info:{P002}" 3600 '{"product_id":"P002","sku":"SKU0002","barcode":"6901234567891","name":"康师傅冰红茶500ml","category_id":"CAT001","price":300,"cost":150,"is_active":1}'
$REDIS_CMD SETEX "product:info:{P003}" 3600 '{"product_id":"P003","sku":"SKU0003","barcode":"6901234567892","name":"可口可乐330ml","category_id":"CAT001","price":250,"cost":120,"is_active":1}'
$REDIS_CMD SETEX "product:info:{P004}" 3600 '{"product_id":"P004","sku":"SKU0004","barcode":"6901234567893","name":"乐事薯片原味75g","category_id":"CAT002","price":650,"cost":320,"is_active":1}'
$REDIS_CMD SETEX "product:info:{P005}" 3600 '{"product_id":"P005","sku":"SKU0005","barcode":"6901234567894","name":"士力架巧克力51g","category_id":"CAT003","price":500,"cost":250,"is_active":1}'

echo "Redis initialization completed (HashTag enabled for Redis Cluster)!"
