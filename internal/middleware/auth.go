package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/auth"
)

// Context keys for JWT claims.
const (
	ContextKeyUserID   = "user_id"
	ContextKeyTenantID = "tenant_id"
	ContextKeyEmail    = "email"
)

// AuthMiddleware validates JWT tokens and injects claims into the request context.
func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
			})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization format, expected: Bearer <token>",
			})
			return
		}
		tokenString := parts[1]

		claims, err := auth.ParseToken(tokenString, secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}

		c.Set(ContextKeyUserID, claims.UserID)
		c.Set(ContextKeyTenantID, claims.TenantID)
		c.Set(ContextKeyEmail, claims.Email)

		c.Next()
	}
}

// GetUserID retrieves the user ID from the request context.
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

// GetTenantID retrieves the tenant ID from the request context.
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

// GetEmail retrieves the email from the request context.
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
