package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/models"
	"go.uber.org/zap"
)

// --- channel mock ---

type mockChannelRepo struct {
	createFn  func(ctx context.Context, tenantID uuid.UUID, name string, isPrivate bool) (*models.Channel, error)
	getByIDFn func(ctx context.Context, tenantID, channelID uuid.UUID) (*models.Channel, error)
	listFn    func(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]models.Channel, error)
}

func (m *mockChannelRepo) Create(ctx context.Context, tenantID uuid.UUID, name string, isPrivate bool) (*models.Channel, error) {
	if m.createFn != nil {
		return m.createFn(ctx, tenantID, name, isPrivate)
	}
	return &models.Channel{ID: uuid.New(), TenantID: tenantID, Name: name, IsPrivate: isPrivate}, nil
}

func (m *mockChannelRepo) GetByID(ctx context.Context, tenantID, channelID uuid.UUID) (*models.Channel, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, tenantID, channelID)
	}
	return &models.Channel{ID: channelID, TenantID: tenantID, Name: "test"}, nil
}

func (m *mockChannelRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]models.Channel, error) {
	if m.listFn != nil {
		return m.listFn(ctx, tenantID, limit, offset)
	}
	return []models.Channel{}, nil
}

// --- helpers ---

func channelRouter(h *ChannelHandler, uid, tid uuid.UUID) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uid)
		c.Set(middleware.ContextKeyTenantID, tid)
		c.Next()
	})
	r.POST("/v1/channels", h.Create)
	r.GET("/v1/channels", h.List)
	r.GET("/v1/channels/:id", h.GetByID)
	return r
}

// --- tests ---

func TestChannelCreate_Success(t *testing.T) {
	h := NewChannelHandler(&mockChannelRepo{}, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels",
		strings.NewReader(`{"name":"general"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChannelCreate_MissingName(t *testing.T) {
	h := NewChannelHandler(&mockChannelRepo{}, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChannelCreate_NameTooLong(t *testing.T) {
	longName := strings.Repeat("a", 81)
	h := NewChannelHandler(&mockChannelRepo{}, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	body := `{"name":"` + longName + `"}`
	req, _ := http.NewRequest("POST", "/v1/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChannelCreate_Name80Chars(t *testing.T) {
	exactName := strings.Repeat("b", 80)
	h := NewChannelHandler(&mockChannelRepo{}, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	body := `{"name":"` + exactName + `"}`
	req, _ := http.NewRequest("POST", "/v1/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for 80-char name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChannelList_PassesPagination(t *testing.T) {
	var gotLimit, gotOffset int
	repo := &mockChannelRepo{
		listFn: func(_ context.Context, _ uuid.UUID, limit, offset int) ([]models.Channel, error) {
			gotLimit = limit
			gotOffset = offset
			return []models.Channel{}, nil
		},
	}
	h := NewChannelHandler(repo, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels?limit=10&offset=20", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotLimit != 10 {
		t.Fatalf("expected limit 10 passed to repo, got %d", gotLimit)
	}
	if gotOffset != 20 {
		t.Fatalf("expected offset 20 passed to repo, got %d", gotOffset)
	}
}

func TestChannelList_DefaultPagination(t *testing.T) {
	var gotLimit, gotOffset int
	repo := &mockChannelRepo{
		listFn: func(_ context.Context, _ uuid.UUID, limit, offset int) ([]models.Channel, error) {
			gotLimit = limit
			gotOffset = offset
			return []models.Channel{}, nil
		},
	}
	h := NewChannelHandler(repo, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels", nil)
	r.ServeHTTP(w, req)

	if gotLimit != 50 {
		t.Fatalf("expected default limit 50, got %d", gotLimit)
	}
	if gotOffset != 0 {
		t.Fatalf("expected default offset 0, got %d", gotOffset)
	}
}

func TestChannelGetByID_NotFound(t *testing.T) {
	repo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return nil, nil
		},
	}
	h := NewChannelHandler(repo, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestChannelGetByID_InvalidID(t *testing.T) {
	h := NewChannelHandler(&mockChannelRepo{}, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/not-a-uuid", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- membership tests ---

type mockMembershipRepoFull struct {
	addErr    error
	removeErr error
	members   []models.ChannelMember
	listErr   error
	isMember  bool
	memberErr error
}

func (m *mockMembershipRepoFull) AddMember(_ context.Context, _, _ uuid.UUID, _ string) error {
	return m.addErr
}
func (m *mockMembershipRepoFull) RemoveMember(_ context.Context, _, _ uuid.UUID) error {
	return m.removeErr
}
func (m *mockMembershipRepoFull) ListMembers(_ context.Context, _ uuid.UUID, _, _ int) ([]models.ChannelMember, error) {
	return m.members, m.listErr
}
func (m *mockMembershipRepoFull) IsMember(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return m.isMember, m.memberErr
}

func membershipRouter(h *MembershipHandler, uid, tid uuid.UUID) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uid)
		c.Set(middleware.ContextKeyTenantID, tid)
		c.Next()
	})
	r.POST("/v1/channels/:id/join", h.Join)
	r.POST("/v1/channels/:id/leave", h.Leave)
	r.GET("/v1/channels/:id/members", h.ListMembers)
	return r
}

func TestJoin_InvalidRole(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	// Channel repo returns a channel for this tenant
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join",
		strings.NewReader(`{"role":"superadmin"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoin_ValidRoleMember(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join",
		strings.NewReader(`{"role":"member"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoin_ValidRoleAdmin(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join",
		strings.NewReader(`{"role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoin_DefaultRole(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	// No body → defaults to "member"
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with default role, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoin_ChannelNotInTenant(t *testing.T) {
	// Channel repo returns nil (channel not found for this tenant)
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return nil, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+uuid.New().String()+"/join",
		strings.NewReader(`{"role":"member"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant channel, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLeave_ChannelNotInTenant(t *testing.T) {
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return nil, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+uuid.New().String()+"/leave", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant leave, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListMembers_Pagination(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	uid1, uid2 := uuid.New(), uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	memRepo := &mockMembershipRepoFull{
		members: []models.ChannelMember{
			{ChannelID: chID, UserID: uid1, Role: "admin"},
			{ChannelID: chID, UserID: uid2, Role: "member"},
		},
	}
	h := NewMembershipHandler(memRepo, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+chID.String()+"/members?limit=10&offset=0", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var members []models.ChannelMember
	if err := json.NewDecoder(w.Body).Decode(&members); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestListMembers_InvalidChannelID(t *testing.T) {
	chRepo := &mockChannelRepo{}
	h := NewMembershipHandler(&mockMembershipRepoFull{}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/not-a-uuid/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
