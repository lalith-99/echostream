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

// AuthHandler handles signup and login (public endpoints, no JWT required).
type AuthHandler struct {
	userRepo   repository.UserRepository
	tenantRepo repository.TenantRepository
	jwtSecret  string
	logger     *zap.Logger
}

// NewAuthHandler returns an AuthHandler.
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

type authResponse struct {
	Token string `json:"token"`
}

// Signup handles POST /v1/auth/signup
func (h *AuthHandler) Signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

	// Don't reveal whether the email or password was wrong.
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

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
