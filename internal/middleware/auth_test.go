package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/auth"
)

func init() { gin.SetMode(gin.TestMode) }

const testSecret = "test-secret-key"

func validToken(t *testing.T, uid, tid uuid.UUID, email string) string {
	t.Helper()
	tok, err := auth.GenerateToken(uid, tid, email, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	uid := uuid.New()
	tid := uuid.New()
	email := "alice@test.com"
	tok := validToken(t, uid, tid, email)

	r := gin.New()
	r.Use(AuthMiddleware(testSecret))
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id":   GetUserID(c).String(),
			"tenant_id": GetTenantID(c).String(),
			"email":     GetEmail(c),
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	r := gin.New()
	r.Use(AuthMiddleware(testSecret))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	r := gin.New()
	r.Use(AuthMiddleware(testSecret))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Token abc123")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	tok, _ := auth.GenerateToken(uuid.New(), uuid.New(), "e@t.com", testSecret, -time.Hour)

	r := gin.New()
	r.Use(AuthMiddleware(testSecret))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_WrongSecret(t *testing.T) {
	tok, _ := auth.GenerateToken(uuid.New(), uuid.New(), "e@t.com", "secret-A", time.Hour)

	r := gin.New()
	r.Use(AuthMiddleware("secret-B"))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestGetUserID_NotSet(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if got := GetUserID(c); got != uuid.Nil {
		t.Fatalf("expected uuid.Nil, got %s", got)
	}
}

func TestGetTenantID_NotSet(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if got := GetTenantID(c); got != uuid.Nil {
		t.Fatalf("expected uuid.Nil, got %s", got)
	}
}

func TestGetEmail_NotSet(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if got := GetEmail(c); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}
