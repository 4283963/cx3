package middleware

import (
	"cx3/config"
	"cx3/utils"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	ips map[string]*visitor
	mu  sync.RWMutex
	r   rate.Limit
	b   int
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*visitor),
		r:   r,
		b:   b,
	}
}

func (i *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	v, exists := i.ips[ip]
	if !exists {
		limiter := rate.NewLimiter(i.r, i.b)
		i.ips[ip] = &visitor{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func (i *IPRateLimiter) CleanupStaleVisitors() {
	for {
		time.Sleep(time.Minute)
		i.mu.Lock()
		for ip, v := range i.ips {
			if time.Since(v.lastSeen) > 5*time.Minute {
				delete(i.ips, ip)
			}
		}
		i.mu.Unlock()
	}
}

var (
	pickupLimiter *IPRateLimiter
	lockLimiter   *IPRateLimiter
	limiterOnce   sync.Once
)

func InitRateLimiters(cfg *config.RateLimitConfig) {
	limiterOnce.Do(func() {
		pickupRate := rate.Limit(cfg.PickupQPS)
		pickupBurst := cfg.BucketSize
		if pickupBurst <= 0 {
			pickupBurst = cfg.PickupQPS * 2
		}
		pickupLimiter = NewIPRateLimiter(pickupRate, pickupBurst)
		go pickupLimiter.CleanupStaleVisitors()

		lockRate := rate.Limit(cfg.LockQPS)
		lockBurst := cfg.LockQPS * 5
		lockLimiter = NewIPRateLimiter(lockRate, lockBurst)
		go lockLimiter.CleanupStaleVisitors()
	})
}

func PickupRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if pickupLimiter == nil {
			c.Next()
			return
		}
		ip := utils.GetClientIP(c)
		limiter := pickupLimiter.getLimiter(ip)
		if !limiter.Allow() {
			utils.Fail(c, 429, utils.CodeTooManyRequests, "pickup请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}

func LockRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if lockLimiter == nil {
			c.Next()
			return
		}
		ip := utils.GetClientIP(c)
		limiter := lockLimiter.getLimiter(ip)
		if !limiter.Allow() {
			utils.Fail(c, 429, utils.CodeTooManyRequests, "lock请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}
