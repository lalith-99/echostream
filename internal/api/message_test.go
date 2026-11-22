package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/repository"
	"github.com/lalith-99/echostream/internal/service"
	"go.uber.org/zap"
)

func init() { gin.SetMode(gin.TestMode) }

type messageRepoStub struct {
	create func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, *uuid.UUID) (*models.Message, bool, error)
	list   func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, repository.MessagePage) ([]models.Message, error)
}

func (s messageRepoStub) Create(ctx context.Context, tenant, channel, user uuid.UUID, body string, key *uuid.UUID) (*models.Message, bool, error) {
	return s.create(ctx, tenant, channel, user, body, key)
}
func (s messageRepoStub) ListByChannel(ctx context.Context, tenant, channel, user uuid.UUID, page repository.MessagePage) ([]models.Message, error) {
	return s.list(ctx, tenant, channel, user, page)
}

func messageRouter(repo repository.MessageRepository, uid, tid uuid.UUID) *gin.Engine {
	h := NewMessageHandler(service.NewMessageService(repo), zap.NewNop())
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uid)
		c.Set(middleware.ContextKeyTenantID, tid)
		c.Next()
	})
	r.POST("/v1/channels/:id/messages", h.Create)
	r.GET("/v1/channels/:id/messages", h.List)
	return r
}

func TestPostMessageContract(t *testing.T) {
	tenant, user, channel, key := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name, content, channel, header string
		err                            error
		created, callStore             bool
		want                           int
	}{
		{"created without key", "hello", channel.String(), "", nil, true, true, 201},
		{"created with key", "hello", channel.String(), key.String(), nil, true, true, 201},
		{"replayed", "hello", channel.String(), key.String(), nil, false, true, 200},
		{"conflicting key", "changed", channel.String(), key.String(), repository.ErrMessageKeyConflict, false, true, 409},
		{"nonmember", "hello", channel.String(), "", repository.ErrMessageForbidden, false, true, 403},
		{"store error", "hi", channel.String(), "", errors.New("unavailable"), false, true, 500},
		{"invalid key", "hello", channel.String(), "invalid", nil, false, false, 400},
		{"invalid channel", "hello", "invalid", "", nil, false, false, 400},
		{"missing content", "", channel.String(), "", nil, false, false, 400},
		{"blank", "   ", channel.String(), "", nil, false, false, 400},
		{"contains null", "a\x00b", channel.String(), "", nil, false, false, 400},
		{"oversized", strings.Repeat("x", 4001), channel.String(), "", nil, false, false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			r := messageRouter(messageRepoStub{create: func(_ context.Context, gotTenant, gotChannel, gotUser uuid.UUID, content string, gotKey *uuid.UUID) (*models.Message, bool, error) {
				called = true
				if gotTenant != tenant || gotChannel != channel || gotUser != user || content != tc.content || (gotKey == nil) != (tc.header == "") || (gotKey != nil && *gotKey != key) {
					t.Fatalf("unexpected send arguments")
				}
				return &models.Message{ID: 12, Body: content}, tc.created, tc.err
			}}, user, tenant)
			body, err := json.Marshal(map[string]string{"content": tc.content})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/channels/"+tc.channel+"/messages", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			if tc.header != "" {
				req.Header.Set("Idempotency-Key", tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want || called != tc.callStore {
				t.Fatalf("status=%d store called=%v body=%s", w.Code, called, w.Body.String())
			}
		})
	}
}

func TestMessageHistoryContract(t *testing.T) {
	tenant, user, channel := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		query         string
		before, after int64 // after=-1 means absent
		limit, status int
		storeErr      error
	}{
		{"", 0, -1, 50, 200, nil},
		{"?before=10&limit=5", 10, -1, 5, 200, nil},
		{"?after=0", 0, 0, 50, 200, nil},
		{"?after=21&limit=200", 0, 21, 100, 200, nil},
		{"?before=abc", 0, -1, 0, 400, nil},
		{"?after=", 0, -1, 0, 400, nil},
		{"?after=-1", 0, -1, 0, 400, nil},
		{"?after=1&before=0", 0, -1, 0, 400, nil},
		{"?limit=0", 0, -1, 0, 400, nil},
		{"?after=1", 0, 1, 50, 403, repository.ErrMessageForbidden},
	} {
		t.Run(tc.query, func(t *testing.T) {
			r := messageRouter(messageRepoStub{list: func(_ context.Context, gotTenant, gotChannel, gotUser uuid.UUID, p repository.MessagePage) ([]models.Message, error) {
				if gotTenant != tenant || gotChannel != channel || gotUser != user || p.Before != tc.before || p.Limit != tc.limit || (p.After == nil) != (tc.after == -1) || (p.After != nil && *p.After != tc.after) {
					t.Fatal("unexpected history arguments", p)
				}
				return []models.Message{{ID: 12}}, tc.storeErr
			}}, user, tenant)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/channels/"+channel.String()+"/messages"+tc.query, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
			}
			if w.Code == 200 {
				var messages []models.Message
				if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil || len(messages) != 1 || messages[0].ID != 12 {
					t.Fatalf("invalid history response %q", w.Body.String())
				}
			}
		})
	}
}
