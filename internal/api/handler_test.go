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
	"github.com/lalith-99/echostream/internal/presence"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
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
	h := NewChannelHandler(&mockChannelRepo{}, &mockMembershipRepoFull{}, zap.NewNop())
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
	h := NewChannelHandler(&mockChannelRepo{}, &mockMembershipRepoFull{}, zap.NewNop())
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
	h := NewChannelHandler(&mockChannelRepo{}, &mockMembershipRepoFull{}, zap.NewNop())
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
	h := NewChannelHandler(&mockChannelRepo{}, &mockMembershipRepoFull{}, zap.NewNop())
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
	h := NewChannelHandler(repo, &mockMembershipRepoFull{}, zap.NewNop())
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
	h := NewChannelHandler(repo, &mockMembershipRepoFull{}, zap.NewNop())
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
	h := NewChannelHandler(repo, &mockMembershipRepoFull{}, zap.NewNop())
	r := channelRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestChannelGetByID_InvalidID(t *testing.T) {
	h := NewChannelHandler(&mockChannelRepo{}, &mockMembershipRepoFull{}, zap.NewNop())
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
	r.POST("/v1/channels/:id/invite", h.Invite)
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

// ==========================================================================
// Auth handler tests
// ==========================================================================

// --- auth mocks ---

type mockUserRepo struct {
	getByEmailFn func(ctx context.Context, email string) (*models.User, error)
	createFn     func(ctx context.Context, tenantID uuid.UUID, email, displayName, passwordHash string) (*models.User, error)
	getByIDFn    func(ctx context.Context, tenantID, userID uuid.UUID) (*models.User, error)
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	if m.getByEmailFn != nil {
		return m.getByEmailFn(ctx, email)
	}
	return nil, nil // default: user not found
}

func (m *mockUserRepo) Create(ctx context.Context, tenantID uuid.UUID, email, displayName, passwordHash string) (*models.User, error) {
	if m.createFn != nil {
		return m.createFn(ctx, tenantID, email, displayName, passwordHash)
	}
	return &models.User{ID: uuid.New(), TenantID: tenantID, Email: email, DisplayName: displayName}, nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, tenantID, userID uuid.UUID) (*models.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, tenantID, userID)
	}
	return nil, nil
}

type mockSignupRepo struct {
	tenant *models.Tenant
	user   *models.User
	err    error
}

func (m *mockSignupRepo) CreateTenantAndUser(_ context.Context, _, _, _, _ string) (*models.Tenant, *models.User, error) {
	return m.tenant, m.user, m.err
}

const testJWTSecret = "test-secret-key-for-unit-tests"

func authRouter(h *AuthHandler) *gin.Engine {
	r := gin.New()
	r.POST("/v1/auth/signup", h.Signup)
	r.POST("/v1/auth/login", h.Login)
	return r
}

// --- auth tests ---

func TestSignup_Success(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()

	h := NewAuthHandler(
		&mockUserRepo{}, // GetByEmail returns nil → no existing user
		&mockSignupRepo{
			tenant: &models.Tenant{ID: tid, Name: "Acme"},
			user:   &models.User{ID: uid, TenantID: tid, Email: "a@b.com", DisplayName: "Alice"},
		},
		testJWTSecret,
		zap.NewNop(),
	)
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"a@b.com","password":"securepass","display_name":"Alice","tenant_name":"Acme"}`
	req, _ := http.NewRequest("POST", "/v1/auth/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a JWT token in response")
	}
}

func TestSignup_DuplicateEmail(t *testing.T) {
	existing := &models.User{ID: uuid.New(), Email: "taken@b.com"}
	h := NewAuthHandler(
		&mockUserRepo{
			getByEmailFn: func(_ context.Context, _ string) (*models.User, error) {
				return existing, nil
			},
		},
		&mockSignupRepo{},
		testJWTSecret,
		zap.NewNop(),
	)
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"taken@b.com","password":"securepass","display_name":"Bob","tenant_name":"Acme"}`
	req, _ := http.NewRequest("POST", "/v1/auth/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignup_MissingFields(t *testing.T) {
	h := NewAuthHandler(&mockUserRepo{}, &mockSignupRepo{}, testJWTSecret, zap.NewNop())
	r := authRouter(h)

	// missing tenant_name
	w := httptest.NewRecorder()
	body := `{"email":"a@b.com","password":"securepass","display_name":"Alice"}`
	req, _ := http.NewRequest("POST", "/v1/auth/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignup_ShortPassword(t *testing.T) {
	h := NewAuthHandler(&mockUserRepo{}, &mockSignupRepo{}, testJWTSecret, zap.NewNop())
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"a@b.com","password":"short","display_name":"Alice","tenant_name":"Acme"}`
	req, _ := http.NewRequest("POST", "/v1/auth/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSignup_RepoError(t *testing.T) {
	h := NewAuthHandler(
		&mockUserRepo{},
		&mockSignupRepo{err: errors.New("db down")},
		testJWTSecret,
		zap.NewNop(),
	)
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"a@b.com","password":"securepass","display_name":"Alice","tenant_name":"Acme"}`
	req, _ := http.NewRequest("POST", "/v1/auth/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestLogin_Success(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	uid, tid := uuid.New(), uuid.New()

	h := NewAuthHandler(
		&mockUserRepo{
			getByEmailFn: func(_ context.Context, _ string) (*models.User, error) {
				return &models.User{
					ID:           uid,
					TenantID:     tid,
					Email:        "a@b.com",
					PasswordHash: string(hash),
				}, nil
			},
		},
		&mockSignupRepo{},
		testJWTSecret,
		zap.NewNop(),
	)
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"a@b.com","password":"correctpass"}`
	req, _ := http.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a JWT token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)

	h := NewAuthHandler(
		&mockUserRepo{
			getByEmailFn: func(_ context.Context, _ string) (*models.User, error) {
				return &models.User{
					ID:           uuid.New(),
					TenantID:     uuid.New(),
					Email:        "a@b.com",
					PasswordHash: string(hash),
				}, nil
			},
		},
		&mockSignupRepo{},
		testJWTSecret,
		zap.NewNop(),
	)
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"a@b.com","password":"wrongpass1"}`
	req, _ := http.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	h := NewAuthHandler(
		&mockUserRepo{}, // GetByEmail returns nil
		&mockSignupRepo{},
		testJWTSecret,
		zap.NewNop(),
	)
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"nobody@b.com","password":"whatever1"}`
	req, _ := http.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_MissingFields(t *testing.T) {
	h := NewAuthHandler(&mockUserRepo{}, &mockSignupRepo{}, testJWTSecret, zap.NewNop())
	r := authRouter(h)

	w := httptest.NewRecorder()
	body := `{"email":"a@b.com"}`
	req, _ := http.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ==========================================================================
// Private channel enforcement tests
// ==========================================================================

func TestJoin_PrivateChannel_NotInvited(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid, IsPrivate: true}, nil
		},
	}
	// isMember defaults to false → user hasn't been invited
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: false}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join",
		strings.NewReader(`{"role":"member"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for private channel without invite, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoin_PrivateChannel_Invited(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid, IsPrivate: true}, nil
		},
	}
	// isMember=true → user was previously invited
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: true}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join",
		strings.NewReader(`{"role":"member"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for invited user joining private channel, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoin_PublicChannel_NoMembershipCheckNeeded(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid, IsPrivate: false}, nil
		},
	}
	// isMember=false, but public channels don't check membership
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: false}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/join",
		strings.NewReader(`{"role":"member"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for public channel join, got %d: %s", w.Code, w.Body.String())
	}
}

// ==========================================================================
// Invite endpoint tests
// ==========================================================================

func TestInvite_Success(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	callerID := uuid.New()
	targetID := uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid, IsPrivate: true}, nil
		},
	}
	// caller is a member
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: true}, chRepo, zap.NewNop())
	r := membershipRouter(h, callerID, tid)

	w := httptest.NewRecorder()
	body := `{"user_id":"` + targetID.String() + `"}`
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/invite",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for valid invite, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvite_CallerNotMember(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	targetID := uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	// caller is NOT a member
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: false}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	body := `{"user_id":"` + targetID.String() + `"}`
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/invite",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member inviting, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvite_InvalidUserID(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: true}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	body := `{"user_id":"not-a-uuid"}`
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/invite",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid user_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvite_MissingBody(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: true}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels/"+chID.String()+"/invite", nil)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvite_ChannelNotFound(t *testing.T) {
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return nil, nil
		},
	}
	h := NewMembershipHandler(&mockMembershipRepoFull{isMember: true}, chRepo, zap.NewNop())
	r := membershipRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	body := `{"user_id":"` + uuid.New().String() + `"}`
	req, _ := http.NewRequest("POST", "/v1/channels/"+uuid.New().String()+"/invite",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for channel not found, got %d: %s", w.Code, w.Body.String())
	}
}

// ==========================================================================
// Channel create auto-adds creator as admin
// ==========================================================================

func TestChannelCreate_AddsCreatorAsAdmin(t *testing.T) {
	uid := uuid.New()
	tid := uuid.New()
	createdCh := &models.Channel{ID: uuid.New(), TenantID: tid, Name: "secret"}

	var addedChannelID, addedUserID uuid.UUID
	var addedRole string

	memRepo := &mockMembershipRepoFull{}
	// Override AddMember to capture the call
	customMemRepo := &capturingMembershipRepo{
		MembershipRepoFull: memRepo,
		onAdd: func(chID, uID uuid.UUID, role string) {
			addedChannelID = chID
			addedUserID = uID
			addedRole = role
		},
	}

	chRepo := &mockChannelRepo{
		createFn: func(_ context.Context, _ uuid.UUID, _ string, _ bool) (*models.Channel, error) {
			return createdCh, nil
		},
	}
	h := NewChannelHandler(chRepo, customMemRepo, zap.NewNop())
	r := channelRouter(h, uid, tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/channels",
		strings.NewReader(`{"name":"secret","is_private":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if addedChannelID != createdCh.ID {
		t.Fatalf("expected AddMember called with channel %s, got %s", createdCh.ID, addedChannelID)
	}
	if addedUserID != uid {
		t.Fatalf("expected AddMember called with user %s, got %s", uid, addedUserID)
	}
	if addedRole != "admin" {
		t.Fatalf("expected AddMember called with role 'admin', got '%s'", addedRole)
	}
}

// capturingMembershipRepo wraps mockMembershipRepoFull but captures AddMember calls.
type capturingMembershipRepo struct {
	*mockMembershipRepoFull
	MembershipRepoFull *mockMembershipRepoFull
	onAdd              func(channelID, userID uuid.UUID, role string)
}

func (c *capturingMembershipRepo) AddMember(_ context.Context, channelID, userID uuid.UUID, role string) error {
	if c.onAdd != nil {
		c.onAdd(channelID, userID, role)
	}
	return nil
}

func (c *capturingMembershipRepo) RemoveMember(ctx context.Context, chID, uID uuid.UUID) error {
	return c.MembershipRepoFull.RemoveMember(ctx, chID, uID)
}

func (c *capturingMembershipRepo) ListMembers(ctx context.Context, chID uuid.UUID, limit, offset int) ([]models.ChannelMember, error) {
	return c.MembershipRepoFull.ListMembers(ctx, chID, limit, offset)
}

func (c *capturingMembershipRepo) IsMember(ctx context.Context, chID, uID uuid.UUID) (bool, error) {
	return c.MembershipRepoFull.IsMember(ctx, chID, uID)
}

// ==========================================================================
// Presence handler tests
// ==========================================================================

type mockPresenceChecker struct {
	statuses map[uuid.UUID]presence.Status
}

func (m *mockPresenceChecker) BulkStatus(_ context.Context, userIDs []uuid.UUID) map[uuid.UUID]presence.Status {
	if m.statuses == nil {
		return nil
	}
	result := make(map[uuid.UUID]presence.Status, len(userIDs))
	for _, id := range userIDs {
		if s, ok := m.statuses[id]; ok {
			result[id] = s
		}
	}
	return result
}

func presenceRouter(h *PresenceHandler, uid, tid uuid.UUID) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uid)
		c.Set(middleware.ContextKeyTenantID, tid)
		c.Next()
	})
	r.GET("/v1/channels/:id/presence", h.GetChannelPresence)
	return r
}

func TestPresence_ChannelNotFound(t *testing.T) {
	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return nil, nil
		},
	}
	h := NewPresenceHandler(chRepo, &mockMembershipRepoFull{}, &mockPresenceChecker{}, zap.NewNop())
	r := presenceRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+uuid.New().String()+"/presence", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPresence_InvalidChannelID(t *testing.T) {
	h := NewPresenceHandler(&mockChannelRepo{}, &mockMembershipRepoFull{}, &mockPresenceChecker{}, zap.NewNop())
	r := presenceRouter(h, uuid.New(), uuid.New())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/not-a-uuid/presence", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPresence_Success(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()
	uid1, uid2, uid3 := uuid.New(), uuid.New(), uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	memRepo := &mockMembershipRepoFull{
		members: []models.ChannelMember{
			{ChannelID: chID, UserID: uid1, Role: "admin"},
			{ChannelID: chID, UserID: uid2, Role: "member"},
			{ChannelID: chID, UserID: uid3, Role: "member"},
		},
	}
	tracker := &mockPresenceChecker{
		statuses: map[uuid.UUID]presence.Status{
			uid1: presence.Online,
			// uid2 is missing → should show "offline"
			uid3: presence.Online,
		},
	}
	h := NewPresenceHandler(chRepo, memRepo, tracker, zap.NewNop())
	r := presenceRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+chID.String()+"/presence", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []memberPresence
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 members, got %d", len(result))
	}

	// Build a lookup by user ID for assertion order independence.
	byUser := make(map[uuid.UUID]memberPresence)
	for _, mp := range result {
		byUser[mp.UserID] = mp
	}

	if byUser[uid1].Status != presence.Online {
		t.Fatalf("uid1 expected online, got %s", byUser[uid1].Status)
	}
	if byUser[uid2].Status != presence.Offline {
		t.Fatalf("uid2 expected offline, got %s", byUser[uid2].Status)
	}
	if byUser[uid3].Status != presence.Online {
		t.Fatalf("uid3 expected online, got %s", byUser[uid3].Status)
	}
	if byUser[uid1].Role != "admin" {
		t.Fatalf("uid1 expected role admin, got %s", byUser[uid1].Role)
	}
}

func TestPresence_EmptyChannel(t *testing.T) {
	tid := uuid.New()
	chID := uuid.New()

	chRepo := &mockChannelRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.Channel, error) {
			return &models.Channel{ID: chID, TenantID: tid}, nil
		},
	}
	// No members
	memRepo := &mockMembershipRepoFull{members: []models.ChannelMember{}}
	h := NewPresenceHandler(chRepo, memRepo, &mockPresenceChecker{}, zap.NewNop())
	r := presenceRouter(h, uuid.New(), tid)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/channels/"+chID.String()+"/presence", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []memberPresence
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 members, got %d", len(result))
	}
}
