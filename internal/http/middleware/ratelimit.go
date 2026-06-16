package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/pkg/response"
	"github.com/redis/go-redis/v9"
)

// RateLimitMiddleware applies a sliding-window rate limit using Redis Sorted Sets.
func RateLimitMiddleware(rdb *redis.Client, keyGen func(*gin.Context) string, limit int, window time.Duration, errorCode string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		key := fmt.Sprintf("rate_limit:%s", keyGen(c))

		now := time.Now().UnixNano()
		clearBefore := now - window.Nanoseconds()

		// Execute Redis commands in an atomic TxPipeline to optimize performance
		pipe := rdb.TxPipeline()
		
		// 1. Remove old logged hits outside the current sliding-window duration
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", clearBefore))
		// 2. Record current access timestamp
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)})
		// 3. Count remaining valid entries (hits within the window)
		pipe.ZCard(ctx, key)
		// 4. Extend key expiration to match window size
		pipe.Expire(ctx, key, window)

		// Execute transaction
		cmds, err := pipe.Exec(ctx)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "RATE_LIMIT_ERROR", "Rate limiter service unavailable: "+err.Error())
			return
		}

		// Retrieve ZCard results (3rd command queue, index 2)
		cardCmd := cmds[2].(*redis.IntCmd)
		hits, err := cardCmd.Result()
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "RATE_LIMIT_ERROR", "Failed to retrieve rate limit details")
			return
		}

		// Check if request count exceeds allowed threshold
		if int(hits) > limit {
			response.Error(c, http.StatusTooManyRequests, errorCode, fmt.Sprintf("Too many requests. Limit is %d per %s.", limit, window))
			return
		}

		c.Next()
	}
}

// IPKeyGenerator creates a rate limit key based on the Client IP and request route path.
func IPKeyGenerator(c *gin.Context) string {
	return fmt.Sprintf("ip:%s:path:%s", c.ClientIP(), c.FullPath())
}

// LoginKeyGenerator creates a dedicated rate limit key based on client IP for brute-force defense on /login.
func LoginKeyGenerator(c *gin.Context) string {
	return fmt.Sprintf("login:ip:%s", c.ClientIP())
}

// RateLimit defines maximum hits and window duration.
type RateLimit struct {
	Max    int
	Window time.Duration
}

// EndpointLimits defines the endpoints mapped to their rate limit configuration.
var EndpointLimits = map[string]RateLimit{
	"/api/v1/auth/login":      {Max: 5, Window: 15 * time.Minute},
	"/api/v1/auth/register":   {Max: 3, Window: 1 * time.Hour},
	"/api/v1/rooms/:id/pin":   {Max: 10, Window: 1 * time.Minute},
	"/api/v1/rooms/:id/prune": {Max: 1, Window: 5 * time.Minute},
	"/api/v1/files/presign":   {Max: 20, Window: 1 * time.Minute},
	"/api/v1/search/messages": {Max: 30, Window: 1 * time.Minute},
	"/api/v1/search/users":    {Max: 30, Window: 1 * time.Minute},
	"/api/v1/search/rooms":    {Max: 30, Window: 1 * time.Minute},
}

// DynamicRateLimitMiddleware automatically intercepts routes defined in EndpointLimits and applies rate limiting.
func DynamicRateLimitMiddleware(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		fullPath := c.FullPath()
		limitConfig, exists := EndpointLimits[fullPath]
		if !exists {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		userIDVal, ok := c.Get("user_id")
		identifier := ""
		if ok {
			identifier = "user:" + userIDVal.(string)
		} else {
			identifier = "ip:" + c.ClientIP()
		}

		key := fmt.Sprintf("rate_limit:%s:path:%s", identifier, fullPath)
		now := time.Now().UnixNano()
		clearBefore := now - limitConfig.Window.Nanoseconds()

		pipe := rdb.TxPipeline()
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", clearBefore))
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)})
		pipe.ZCard(ctx, key)
		pipe.Expire(ctx, key, limitConfig.Window)

		cmds, err := pipe.Exec(ctx)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "RATE_LIMIT_ERROR", "Rate limiter service unavailable: "+err.Error())
			c.Abort()
			return
		}

		cardCmd := cmds[2].(*redis.IntCmd)
		hits, err := cardCmd.Result()
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "RATE_LIMIT_ERROR", "Failed to retrieve rate limit details")
			c.Abort()
			return
		}

		if int(hits) > limitConfig.Max {
			response.Error(c, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", fmt.Sprintf("Too many requests. Limit is %d per %s.", limitConfig.Max, limitConfig.Window))
			c.Abort()
			return
		}

		c.Next()
	}
}
