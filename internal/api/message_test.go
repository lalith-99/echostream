package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/models"
	"go.uber.org/zap"
)

func init() { gin.SetMode(gin.TestMode) }

// --- mocks ---

type mockMessageRepo struct {
	createFn func(ctx context.Context, tenantID, channelID, senderID uuid.UUID, body string) (*models.Message, error)
	listFn   func(ctx context.Context, tenantID, channelID uuid.UUID, before int64, limit int) ([]models.Message, error)
}

func (m *mockMessageRepo) Create(ctx context.Context, tenantID, channelID, senderID uuid.UUID, body string) (*models.Message, error) {
	if m.createFn != nil {
		return m.createFn(ctx, tenantID, channelID, senderID, body)
	}
	return &models.Message{
		ID:        1,
		ChannelID: channelID,
		SenderID:  senderID,
		Body:      body,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockMessageRepo) ListByChannel(ctx context.Context, tenantID, channelID uuid.UUID, before int64, limit int) ([]models.Message, error) {
	if m.listFn != nil {
		return m.listFn(ctx, tenantID, channelID, before, limit)
	}
	return []models.Message{}, nil
}

type mockPublisher struct {
	published []struct {
		channel string
		payload []byte
	}
	err error
}

func (m *mockPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	m.published = append(m.published, struct {
		channel string
		payload []byte
	}{channel, payload})
	return m.err
}

// setupRouter creates a gin router with fake auth context injected.
func setupRouter(h *MessageHandler, uid, tid uuid.UUID) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uid)
		c.Set(middleware.ContextKeyTenantID, tid)
		c.Set(middleware.ContextKeyEmail, "test@test.com")
		c.Next()
	})
	r.POST("/v1/channels/:id/messages", h.Create)
	r.GET("/v1/channels/:id/messages", h.List)
	return r
}

// --- tests ---

func TestCreate_Success(t *testing.T) {
	uid, tid, chID := uuid.New(), uuid.New(), uuid.New()
	pub := &mockPublisher{}
	h := NewMessageHandler(&mockMessageRepo{}, pub, zap.NewNop())
	r := setupRouter(h, uid, tid)

	body := `{"content":"hello"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Publisher should have been called once with correct channel key
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(pub.published))
	}
	if pub.published[0].channel != "ch:"+chID.String() {
		t.Fatalf("wrong channel: %s", pub.published[0].channel)
	}
}

func TestCreate_MissingContent(t *testing.T) {
	h := NewMessageHandler(&mockMessageRepo{}, nil, zap.NewNop())
	r := setupRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+uuid.New().String()+"/messages",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreate_InvalidChannelID(t *testing.T) {
	h := NewMessageHandler(&mockMessageRepo{}, nil, zap.NewNop())
	r := setupRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/not-a-uuid/messages",
		strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreate_RepoError(t *testing.T) {
	repo := &mockMessageRepo{
		createFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (*models.Message, error) {
			return nil, errors.New("db down")
		},
	}
	h := NewMessageHandler(repo, nil, zap.NewNop())
	r := setupRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+uuid.New().String()+"/messages",
		strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreate_NilPublisher(t *testing.T) {
	h := NewMessageHandler(&mockMessageRepo{}, nil, zap.NewNop())
	r := setupRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+uuid.New().String()+"/messages",
		strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestList_Success(t *testing.T) {
	chID := uuid.New()
	repo := &mockMessageRepo{
		listFn: func(_ context.Context, _, _ uuid.UUID, _ int64, _ int) ([]models.Message, error) {
			return []models.Message{
				{ID: 2, ChannelID: chID, Body: "second"},
				{ID: 1, ChannelID: chID, Body: "first"},
			}, nil
		},
	}
	h := NewMessageHandler(repo, nil, zap.NewNop())
	r := setupRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+chID.String()+"/messages", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var msgs []models.Message
	if err := json.NewDecoder(w.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestList_InvalidBefore(t *testing.T) {
	h := NewMessageHandler(&mockMessageRepo{}, nil, zap.NewNop())
	r := setupRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+uuid.New().String()+"/messages?before=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
