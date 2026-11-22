package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

// UserHandler handles user-related operations.
type UserHandler struct {
	repo   repository.UserRepository
	logger *zap.Logger
}

// NewUserHandler returns a handler for user endpoints.
func NewUserHandler(repo repository.UserRepository, logger *zap.Logger) *UserHandler {
	return &UserHandler{repo: repo, logger: logger}
}

// GetMe handles GET /v1/users/me
func (h *UserHandler) GetMe(c *gin.Context) {
	userID := middleware.GetUserID(c)
	tenantID := middleware.GetTenantID(c)

	user, err := h.repo.GetByID(c.Request.Context(), tenantID, userID)
	if err != nil {
		h.logger.Error("failed to get user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user"})
		return
	}

	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// List handles GET /v1/users?limit=100&offset=0.
func (h *UserHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	limit, offset := parsePagination(c, 100, 200)
	users, err := h.repo.ListByTenant(c.Request.Context(), tenantID, middleware.GetUserID(c), limit, offset)
	if err != nil {
		h.logger.Error("failed to list users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	c.JSON(http.StatusOK, users)
}
