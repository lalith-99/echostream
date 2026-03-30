package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// callParsePagination is a helper that invokes parsePagination inside a fake Gin context
// built from the given raw query string (e.g. "limit=10&offset=5").
func callParsePagination(query string, defaultLimit, maxLimit int) (limit, offset int) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/?"+query, nil)
	return parsePagination(c, defaultLimit, maxLimit)
}

func TestParsePagination_Defaults(t *testing.T) {
	limit, offset := callParsePagination("", 50, 100)
	if limit != 50 {
		t.Fatalf("expected default limit 50, got %d", limit)
	}
	if offset != 0 {
		t.Fatalf("expected default offset 0, got %d", offset)
	}
}

func TestParsePagination_ExplicitValues(t *testing.T) {
	limit, offset := callParsePagination("limit=20&offset=40", 50, 100)
	if limit != 20 {
		t.Fatalf("expected limit 20, got %d", limit)
	}
	if offset != 40 {
		t.Fatalf("expected offset 40, got %d", offset)
	}
}

func TestParsePagination_ClampToMax(t *testing.T) {
	limit, _ := callParsePagination("limit=999", 50, 100)
	if limit != 100 {
		t.Fatalf("expected clamped limit 100, got %d", limit)
	}
}

func TestParsePagination_InvalidLimit(t *testing.T) {
	limit, _ := callParsePagination("limit=abc", 50, 100)
	if limit != 50 {
		t.Fatalf("expected default limit 50 on invalid input, got %d", limit)
	}
}

func TestParsePagination_NegativeLimit(t *testing.T) {
	limit, _ := callParsePagination("limit=-5", 50, 100)
	if limit != 50 {
		t.Fatalf("expected default limit 50 on negative, got %d", limit)
	}
}

func TestParsePagination_NegativeOffset(t *testing.T) {
	_, offset := callParsePagination("offset=-10", 50, 100)
	if offset != 0 {
		t.Fatalf("expected 0 on negative offset, got %d", offset)
	}
}

func TestParsePagination_ZeroLimit(t *testing.T) {
	limit, _ := callParsePagination("limit=0", 50, 100)
	if limit != 50 {
		t.Fatalf("expected default limit on zero, got %d", limit)
	}
}
