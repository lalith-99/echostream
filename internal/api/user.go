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

func NewUserHandler(repo repository.UserRepository, logger *zap.Logger) *UserHandler {
	return &UserHandler{repo: repo, logger: logger}
}

// GetMe handles GET /v1/users/me
//
// Returns the currently authenticated user's profile.
//
// Why /users/me and not /users/:id?
//   - /users/me is idiomatic for "get my own profile". Client doesn't need
//     to know their own UUID — they just call /users/me and get themselves.
//   - /users/:id would be for fetching OTHER users' profiles, which we'll
//     add later when we need user search or mentions.
func (h *UserHandler) GetMe(c *gin.Context) {
	userID := middleware.GetUserID(c)
	tenantID := middleware.GetTenantID(c)

	user, err := h.repo.GetByID(c.Request.Context(), tenantID, userID)
	if err != nil {
		h.logger.Error("failed to get user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user"})
		return
	}

	// If the user is in the JWT but not in the DB, that's a data consistency
	// bug. We'd normally create the user during signup/login, so this
	// shouldn't happen. Return 404 instead of 500.
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}
