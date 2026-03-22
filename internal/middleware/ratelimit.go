package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// RateLimiter returns middleware that limits each user to `limit` requests
// per `window` using a Redis counter.
//
// How it works:
//
//  1. Build a key like "rl:<userID>:<currentMinute>"
//  2. INCR the key in Redis (atomically increments, creates if missing)
//  3. If this is the first request in the window, set TTL = window duration
//  4. If count > limit, respond 429 Too Many Requests
//
// Why Redis instead of in-memory?
//   - Works across multiple server instances
//   - No memory leak from accumulating user counters
//   - Keys auto-expire via TTL
//
// Why sliding window approximation?
//   - True sliding window needs sorted sets (complex)
//   - Fixed window with per-minute keys is simple, predictable, and good enough
func RateLimiter(rdb *goredis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := GetUserID(c)
		if userID.String() == "00000000-0000-0000-0000-000000000000" {
			// No user in context (public route) → skip rate limiting
			c.Next()
			return
		}

		// Key format: "rl:<userID>:<window_bucket>"
		// The bucket number changes each window, so old keys expire naturally.
		bucket := time.Now().Unix() / int64(window.Seconds())
		key := fmt.Sprintf("rl:%s:%d", userID.String(), bucket)

		ctx := context.Background()

		// INCR is atomic: creates key with value 1 if it doesn't exist,
		// or increments the existing value. Either way, returns the new count.
		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			// Redis down → fail open (allow the request through).
			// Availability > strictness for rate limiting.
			c.Next()
			return
		}

		// First request in this window → set TTL so the key auto-cleans.
		if count == 1 {
			rdb.Expire(ctx, key, window)
		}

		// Set rate limit headers so clients know their budget
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, int64(limit)-count)))

		if count > int64(limit) {
			c.Header("Retry-After", fmt.Sprintf("%d", int64(window.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
