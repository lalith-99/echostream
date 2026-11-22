package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/auth"
	"github.com/lalith-99/echostream/internal/invite"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler handles signup and login (public endpoints, no JWT required).
type AuthHandler struct {
	userRepo   repository.UserRepository
	signupRepo repository.SignupRepository
	inviteSvc  *invite.Service
	jwtSecret  string
	logger     *zap.Logger
}

// NewAuthHandler returns an AuthHandler.
func NewAuthHandler(
	userRepo repository.UserRepository,
	signupRepo repository.SignupRepository,
	inviteSvc *invite.Service,
	jwtSecret string,
	logger *zap.Logger,
) *AuthHandler {
	return &AuthHandler{
		userRepo:   userRepo,
		signupRepo: signupRepo,
		inviteSvc:  inviteSvc,
		jwtSecret:  jwtSecret,
		logger:     logger,
	}
}

type signupRequest struct {
	Email       string `json:"email" binding:"required,email,max=254"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name" binding:"required,max=120"`
	// TenantName is required when InviteToken is empty (creates a new workspace).
	TenantName  string `json:"tenant_name" binding:"max=120"`
	InviteToken string `json:"invite_token"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required"`
}

type authResponse struct {
	Token string `json:"token"`
}

// Signup handles POST /v1/auth/signup
//
// Creates tenant + user inside a single Postgres transaction.
// If either INSERT fails, both roll back — no orphan tenants.
func (h *AuthHandler) Signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.TenantName = strings.TrimSpace(req.TenantName)
	if req.DisplayName == "" || (req.InviteToken == "" && req.TenantName == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "display name and workspace name or invite are required"})
		return
	}
	if len(req.Password) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password exceeds 72 bytes"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error("failed to hash password", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}

	var userID, tenantID uuid.UUID

	if req.InviteToken != "" {
		// Join an existing workspace via invite token.
		tenantIDStr, err := h.inviteSvc.Resolve(c.Request.Context(), req.InviteToken)
		if err != nil {
			if errors.Is(err, invite.ErrNotFound) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invite link is invalid or expired"})
				return
			}
			h.logger.Error("failed to resolve invite token", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
			return
		}
		parsedTenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invite link is invalid or expired"})
			return
		}
		user, err := h.userRepo.Create(c.Request.Context(), parsedTenantID, req.Email, req.DisplayName, string(hash))
		if err != nil {
			h.signupError(c, err)
			return
		}
		userID, tenantID = user.ID, user.TenantID
	} else {
		// Create a brand-new workspace + user atomically.
		tenant, user, err := h.signupRepo.CreateTenantAndUser(c.Request.Context(), req.TenantName, req.Email, req.DisplayName, string(hash))
		if err != nil {
			h.signupError(c, err)
			return
		}
		userID, tenantID = user.ID, tenant.ID
	}

	token, err := auth.GenerateToken(userID, tenantID, req.Email, h.jwtSecret, 24*time.Hour)
	if err != nil {
		h.logger.Error("failed to generate token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, authResponse{Token: token})
}

func (h *AuthHandler) signupError(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrEmailRegistered) {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}
	h.logger.Error("failed to create account", zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{"error": "signup failed"})
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

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, authResponse{Token: token})
}
