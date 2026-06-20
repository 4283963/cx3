//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	ctx := context.Background()

	redisClient := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis connect failed: %v", err)
	}
	defer redisClient.Close()

	dsn := "root:root@tcp(127.0.0.1:3306)/cx3_shelf?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Printf("mysql connect failed (skipped mysql init): %v", err)
	} else {
		log.Println("MySQL connected")
		_ = db
	}

	stockData := map[string]int{
		"shelf:stock:SH001:1": 25,
		"shelf:stock:SH001:2": 18,
		"shelf:stock:SH001:3": 30,
		"shelf:stock:SH001:4": 12,
		"shelf:stock:SH001:5": 20,
		"shelf:stock:SH002:1": 28,
		"shelf:stock:SH002:2": 15,
		"shelf:stock:SH003:1": 22,
	}
	for key, val := range stockData {
		if err := redisClient.Set(ctx, key, val, 0).Err(); err != nil {
			log.Printf("set %s failed: %v", key, err)
		}
	}

	productMap := map[string]string{
		"shelf:product:SH001:1": "P001",
		"shelf:product:SH001:2": "P002",
		"shelf:product:SH001:3": "P003",
		"shelf:product:SH001:4": "P004",
		"shelf:product:SH001:5": "P005",
		"shelf:product:SH002:1": "P001",
		"shelf:product:SH002:2": "P003",
		"shelf:product:SH003:1": "P002",
	}
	for key, val := range productMap {
		if err := redisClient.Set(ctx, key, val, 0).Err(); err != nil {
			log.Printf("set %s failed: %v", key, err)
		}
	}

	products := []struct {
		ProductID  string `json:"product_id"`
		SKU        string `json:"sku"`
		Barcode    string `json:"barcode"`
		Name       string `json:"name"`
		CategoryID string `json:"category_id"`
		Price      int64  `json:"price"`
		Cost       int64  `json:"cost"`
		IsActive   int    `json:"is_active"`
	}{
		{"P001", "SKU0001", "6901234567890", "农夫山泉550ml", "CAT001", 200, 100, 1},
		{"P002", "SKU0002", "6901234567891", "康师傅冰红茶500ml", "CAT001", 300, 150, 1},
		{"P003", "SKU0003", "6901234567892", "可口可乐330ml", "CAT001", 250, 120, 1},
		{"P004", "SKU0004", "6901234567893", "乐事薯片原味75g", "CAT002", 650, 320, 1},
		{"P005", "SKU0005", "6901234567894", "士力架巧克力51g", "CAT003", 500, 250, 1},
	}
	for _, p := range products {
		data, _ := json.Marshal(p)
		key := fmt.Sprintf("product:info:%s", p.ProductID)
		if err := redisClient.Set(ctx, key, data, time.Hour).Err(); err != nil {
			log.Printf("set %s failed: %v", key, err)
		}
	}

	redisClient.Set(ctx, "shelf:status:SH001", 0, 0)
	redisClient.Set(ctx, "shelf:status:SH002", 0, 0)
	redisClient.Set(ctx, "shelf:status:SH003", 0, 0)
	redisClient.Set(ctx, "shelf:etag:SH001", 1, 0)
	redisClient.Set(ctx, "shelf:etag:SH002", 1, 0)
	redisClient.Set(ctx, "shelf:etag:SH003", 1, 0)

	log.Println("Test data initialization completed!")
}
