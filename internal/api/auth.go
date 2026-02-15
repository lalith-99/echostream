package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/auth"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler handles signup and login — the only PUBLIC endpoints.
// These don't go through AuthMiddleware because the user doesn't have
// a JWT yet (that's what these endpoints produce).
type AuthHandler struct {
	userRepo   repository.UserRepository
	tenantRepo repository.TenantRepository
	jwtSecret  string
	logger     *zap.Logger
}

func NewAuthHandler(
	userRepo repository.UserRepository,
	tenantRepo repository.TenantRepository,
	jwtSecret string,
	logger *zap.Logger,
) *AuthHandler {
	return &AuthHandler{
		userRepo:   userRepo,
		tenantRepo: tenantRepo,
		jwtSecret:  jwtSecret,
		logger:     logger,
	}
}

type signupRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name" binding:"required"`
	TenantName  string `json:"tenant_name" binding:"required"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// authResponse is what both signup and login return.
// The client stores this token and sends it as "Authorization: Bearer <token>"
// on every subsequent request.
type authResponse struct {
	Token string `json:"token"`
}

// Signup handles POST /v1/auth/signup
//
// Flow:
//   1. Validate input
//   2. Check if email already exists
//   3. Hash the password (NEVER store plaintext)
//   4. Create a tenant (workspace)
//   5. Create the user in that tenant
//   6. Generate a JWT and return it
func (h *AuthHandler) Signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if email is already taken.
	existing, err := h.userRepo.GetByEmail(c.Request.Context(), req.Email)
	if err != nil {
		h.logger.Error("failed to check existing user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}

	// Hash the password with bcrypt.
	//
	// bcrypt.DefaultCost = 10 (2^10 = 1024 iterations).
	// This makes brute-force attacks expensive. Each hash takes ~100ms
	// on modern hardware — fast enough for login, slow enough to stop
	// an attacker trying millions of passwords.
	//
	// bcrypt also generates a unique salt per password automatically.
	// Two users with the same password get different hashes.
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error("failed to hash password", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}

	// Create the tenant first (user references tenant via FK).
	tenant, err := h.tenantRepo.Create(c.Request.Context(), req.TenantName)
	if err != nil {
		h.logger.Error("failed to create tenant", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}

	// Create the user in that tenant.
	user, err := h.userRepo.Create(
		c.Request.Context(),
		tenant.ID,
		req.Email,
		req.DisplayName,
		string(hash),
	)
	if err != nil {
		h.logger.Error("failed to create user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}

	// Generate a JWT valid for 24 hours.
	token, err := auth.GenerateToken(user.ID, tenant.ID, user.Email, h.jwtSecret, 24*time.Hour)
	if err != nil {
		h.logger.Error("failed to generate token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}

	c.JSON(http.StatusCreated, authResponse{Token: token})
}

// Login handles POST /v1/auth/login
//
// Flow:
//   1. Validate input
//   2. Find the user by email
//   3. Compare the password against the stored hash
//   4. Generate a JWT and return it
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find the user by email.
	user, err := h.userRepo.GetByEmail(c.Request.Context(), req.Email)
	if err != nil {
		h.logger.Error("failed to find user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	// Generic error for both "user not found" and "wrong password".
	// NEVER say "user not found" vs "wrong password" separately —
	// that tells an attacker which emails are registered.
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	// bcrypt.CompareHashAndPassword does a constant-time comparison.
	// It's resistant to timing attacks — an attacker can't figure out
	// how many characters of the password they got right.
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	token, err := auth.GenerateToken(user.ID, user.TenantID, user.Email, h.jwtSecret, 24*time.Hour)
	if err != nil {
		h.logger.Error("failed to generate token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	c.JSON(http.StatusOK, authResponse{Token: token})
}
