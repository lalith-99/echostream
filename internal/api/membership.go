package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

// MembershipHandler handles channel membership operations.
type MembershipHandler struct {
	repo   repository.MembershipRepository
	logger *zap.Logger
}

// NewMembershipHandler returns a MembershipHandler.
func NewMembershipHandler(repo repository.MembershipRepository, logger *zap.Logger) *MembershipHandler {
	return &MembershipHandler{repo: repo, logger: logger}
}

type joinChannelRequest struct {
	Role string `json:"role"`
}

// Join handles POST /v1/channels/:id/join
func (h *MembershipHandler) Join(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	userID := middleware.GetUserID(c)

	// Body is optional, default to "member" role.
	var req joinChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Role = "member"
	}
	if req.Role == "" {
		req.Role = "member"
	}

	err = h.repo.AddMember(c.Request.Context(), channelID, userID, req.Role)
	if err != nil {
		h.logger.Error("failed to join channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Leave handles POST /v1/channels/:id/leave
func (h *MembershipHandler) Leave(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	userID := middleware.GetUserID(c)

	err = h.repo.RemoveMember(c.Request.Context(), channelID, userID)
	if err != nil {
		h.logger.Error("failed to leave channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to leave channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// ListMembers handles GET /v1/channels/:id/members
func (h *MembershipHandler) ListMembers(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	members, err := h.repo.ListMembers(c.Request.Context(), channelID)
	if err != nil {
		h.logger.Error("failed to list members", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	c.JSON(http.StatusOK, members)
}
