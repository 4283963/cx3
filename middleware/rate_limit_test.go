package middleware

import (
	"cx3/config"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestIPRateLimiter(t *testing.T) {
	limiter := NewIPRateLimiter(rate.Limit(100), 200)

	tests := []struct {
		name     string
		ip       string
		reqCount int
		rate     rate.Limit
		burst    int
	}{
		{"single ip basic", "192.168.1.1", 150, 100, 200},
		{"multiple ip", "10.0.0.1", 100, 100, 200},
		{"another ip", "172.16.0.1", 50, 100, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.reqCount; i++ {
				l := limiter.getLimiter(tt.ip)
				assert.NotNil(t, l)
			}

			limiter.mu.RLock()
			_, exists := limiter.ips[tt.ip]
			limiter.mu.RUnlock()
			assert.True(t, exists, "IP should be registered")
		})
	}
}

func TestRateLimiters_Init(t *testing.T) {
	cfg := &config.RateLimitConfig{
		PickupQPS:  5000,
		LockQPS:    100,
		BucketSize: 10000,
	}
	InitRateLimiters(cfg)

	assert.NotNil(t, pickupLimiter, "pickup limiter should be initialized")
	assert.NotNil(t, lockLimiter, "lock limiter should be initialized")

	InitRateLimiters(cfg)
	InitRateLimiters(cfg)
}

func TestRateLimitConsistency(t *testing.T) {
	limiter := NewIPRateLimiter(rate.Limit(10), 10)
	ip := "10.0.0.100"

	allowed := 0
	blocked := 0
	l := limiter.getLimiter(ip)

	for i := 0; i < 50; i++ {
		if l.Allow() {
			allowed++
		} else {
			blocked++
		}
	}

	assert.Equal(t, 10, allowed, "Burst size should allow exactly 10 initial requests")
	assert.Equal(t, 40, blocked, "Remaining requests should be blocked")

	time.Sleep(200 * time.Millisecond)
	allowed2 := 0
	for i := 0; i < 5; i++ {
		if l.Allow() {
			allowed2++
		}
	}
	assert.GreaterOrEqual(t, allowed2, 1, "After wait, some new tokens should be available")
}
