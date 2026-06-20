package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	TraceIDKey = "X-Trace-ID"
	RequestIDKey = "X-Request-ID"
	UserIDKey    = "X-User-ID"
)

var (
	sequenceNumber int64
)

func GetTraceID(c *gin.Context) string {
	if traceID, exists := c.Get(TraceIDKey); exists {
		return traceID.(string)
	}
	traceID := c.GetHeader(TraceIDKey)
	if traceID == "" {
		traceID = GenerateTraceID()
	}
	c.Set(TraceIDKey, traceID)
	return traceID
}

func GenerateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func GenerateTransactionID() string {
	now := time.Now()
	seq := atomic.AddInt64(&sequenceNumber, 1) & 0xFFFF
	return fmt.Sprintf("TX%s%04d%05d", now.Format("20060102150405"), now.Nanosecond()/1000%10000, seq)
}

func GenerateLockID() int64 {
	now := time.Now().UnixMilli()
	seq := atomic.AddInt64(&sequenceNumber, 1) & 0xFFFFF
	return (now << 20) | seq
}

func GetClientIP(c *gin.Context) string {
	xForwardedFor := c.GetHeader("X-Forwarded-For")
	if xForwardedFor != "" {
		ips := strings.Split(xForwardedFor, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	xRealIP := c.GetHeader("X-Real-IP")
	if xRealIP != "" {
		if net.ParseIP(xRealIP) != nil {
			return xRealIP
		}
	}
	return c.ClientIP()
}

func GetRequestID(c *gin.Context) string {
	return c.GetHeader(RequestIDKey)
}

func BuildShelfStockKey(shelfID string, slotNo int) string {
	return fmt.Sprintf("shelf:stock:%s:%d", shelfID, slotNo)
}

func BuildShelfProductKey(shelfID string, slotNo int) string {
	return fmt.Sprintf("shelf:product:%s:%d", shelfID, slotNo)
}

func BuildShelfStatusKey(shelfID string) string {
	return fmt.Sprintf("shelf:status:%s", shelfID)
}

func BuildShelfLockKey(shelfID string) string {
	return fmt.Sprintf("shelf:lock:%s", shelfID)
}

func BuildProductInfoKey(productID string) string {
	return fmt.Sprintf("product:info:%s", productID)
}

func BuildETagKey(shelfID string) string {
	return fmt.Sprintf("shelf:etag:%s", shelfID)
}

func BuildIdempotentKey(key string) string {
	return fmt.Sprintf("idempotent:%s", key)
}

func BuildUserPickingKey(userID string, shelfID string) string {
	return fmt.Sprintf("user:picking:%s:%s", userID, shelfID)
}

func NowUnix() int64 {
	return time.Now().Unix()
}

func NowUnixMilli() int64 {
	return time.Now().UnixMilli()
}

type contextKey string

const (
	CtxKeyTraceID contextKey = "trace_id"
)

func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, CtxKeyTraceID, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if v := ctx.Value(CtxKeyTraceID); v != nil {
		return v.(string)
	}
	return ""
}

func GetStatusFromHTTP(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch {
	case strings.Contains(err.Error(), "timeout"):
		return http.StatusGatewayTimeout
	case strings.Contains(err.Error(), "connection refused"):
		return http.StatusServiceUnavailable
	case strings.Contains(err.Error(), "not found"):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
