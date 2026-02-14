package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/auth"
)

// Context keys for storing claims in gin.Context.
//
// Why string constants instead of inline strings?
//   - Typo protection. If you write c.Get("usr_id") by mistake, it compiles
//     fine but silently returns nil. With constants, the compiler catches typos.
//   - Single source of truth: handlers import these constants, so everyone
//     agrees on the same keys.
const (
	ContextKeyUserID   = "user_id"
	ContextKeyTenantID = "tenant_id"
	ContextKeyEmail    = "email"
)

// AuthMiddleware returns a Gin middleware that validates JWT tokens.
//
// How Gin middleware works:
//   - A middleware is a function that returns gin.HandlerFunc.
//   - It runs BEFORE your actual handler (CreateChannel, SendMessage, etc.).
//   - If the token is invalid, it calls c.Abort() — which stops the chain.
//     Your handler never runs. The client gets a 401.
//   - If the token is valid, it stores the claims in c.Set() and calls
//     c.Next() — which passes control to the next handler in the chain.
//
// Why take `secret` as a parameter?
//   - So the middleware doesn't import the config package directly.
//   - The main.go passes cfg.JWTSecret when wiring things up.
//   - This makes the middleware testable: pass any secret in tests.
func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Step 1: Get the Authorization header.
		// Expected format: "Bearer eyJhbGciOi..."
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
			})
			return
		}

		// Step 2: Extract the token string.
		// Split "Bearer eyJhbG..." into ["Bearer", "eyJhbG..."]
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization format, expected: Bearer <token>",
			})
			return
		}
		tokenString := parts[1]

		// Step 3: Parse and validate the token.
		// This checks signature, expiry, and signing method.
		claims, err := auth.ParseToken(tokenString, secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}

		// Step 4: Store claims in the request context.
		//
		// c.Set() puts values into gin's per-request context. Any handler
		// later in the chain can read them with c.Get() or our helper
		// functions below. This is how the handler knows "who am I talking to"
		// without parsing the token again.
		c.Set(ContextKeyUserID, claims.UserID)
		c.Set(ContextKeyTenantID, claims.TenantID)
		c.Set(ContextKeyEmail, claims.Email)

		// Step 5: Continue to the next handler.
		c.Next()
	}
}

// ---------------------------------------------------------------
// Helper functions for handlers to extract claims from context.
//
// Why helpers instead of c.Get("user_id") directly in handlers?
//   - Type safety: c.Get() returns (any, bool). The handler would need
//     to type-assert every time: val, _ := c.Get("user_id"); uid := val.(uuid.UUID)
//   - These helpers do the assertion once, in one place.
//   - If something goes wrong (key missing), they return uuid.Nil — a
//     safe zero value that will fail any DB query gracefully.
// ---------------------------------------------------------------

func GetUserID(c *gin.Context) uuid.UUID {
	val, exists := c.Get(ContextKeyUserID)
	if !exists {
		return uuid.Nil
	}
	id, ok := val.(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return id
}

func GetTenantID(c *gin.Context) uuid.UUID {
	val, exists := c.Get(ContextKeyTenantID)
	if !exists {
		return uuid.Nil
	}
	id, ok := val.(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return id
}

func GetEmail(c *gin.Context) string {
	val, exists := c.Get(ContextKeyEmail)
	if !exists {
		return ""
	}
	email, ok := val.(string)
	if !ok {
		return ""
	}
	return email
}
