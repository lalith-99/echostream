package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS returns middleware that applies Cross-Origin Resource Sharing headers so
// browser-based clients (e.g. the web frontend) can call the API from a
// different origin.
//
// Behavior:
//   - Reflects the request Origin only if it is in the allowlist.
//   - Handles preflight (OPTIONS) requests by short-circuiting with 204.
//   - "*" in the allowlist permits any origin (use only for local dev).
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok || allowAll {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
				c.Header("Access-Control-Expose-Headers", "X-RateLimit-Limit, X-RateLimit-Remaining, Retry-After")
				c.Header("Access-Control-Max-Age", "86400")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
