package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

var incrementWindow = goredis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return count
`)

func RateLimiter(rdb *goredis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return fixedWindow(rdb, limit, window, "user", false, func(c *gin.Context) string {
		return GetUserID(c).String()
	})
}

// Public authentication is CPU intensive. Protect it even when the limiter is unavailable.
func AuthRateLimiter(rdb *goredis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return fixedWindow(rdb, limit, window, "auth", true, func(c *gin.Context) string {
		return c.ClientIP()
	})
}

func fixedWindow(rdb *goredis.Client, limit int, window time.Duration, scope string, failClosed bool, identity func(*gin.Context) string) gin.HandlerFunc {
	seconds := int64(window / time.Second)
	return func(c *gin.Context) {
		now := time.Now().Unix()
		key := fmt.Sprintf("rl:%s:%s:%d", scope, identity(c), now/seconds)
		retryAfter := seconds - now%seconds
		count, err := incrementWindow.Run(c.Request.Context(), rdb, []string{key}, seconds).Int64()
		if err != nil {
			if failClosed {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "authentication temporarily unavailable"})
				return
			}
			c.Next()
			return
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(max(0, int64(limit)-count), 10))
		if count > int64(limit) {
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
