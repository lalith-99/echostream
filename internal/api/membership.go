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
	repo     repository.MembershipRepository
	channels repository.ChannelRepository // needed to verify channel belongs to caller's tenant
	logger   *zap.Logger
}

// NewMembershipHandler returns a MembershipHandler.
func NewMembershipHandler(repo repository.MembershipRepository, channels repository.ChannelRepository, logger *zap.Logger) *MembershipHandler {
	return &MembershipHandler{repo: repo, channels: channels, logger: logger}
}

type joinChannelRequest struct {
	Role string `json:"role"`
}

var validRoles = map[string]bool{
	"member": true,
	"admin":  true,
}

// verifyChannelTenant checks that the channel exists and belongs to the caller's tenant.
// Returns the parsed channelID, or writes an error response and returns uuid.Nil.
func (h *MembershipHandler) verifyChannelTenant(c *gin.Context) uuid.UUID {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return uuid.Nil
	}

	tenantID := middleware.GetTenantID(c)
	ch, err := h.channels.GetByID(c.Request.Context(), tenantID, channelID)
	if err != nil {
		h.logger.Error("failed to verify channel tenant", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return uuid.Nil
	}
	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return uuid.Nil
	}
	return channelID
}

// Join handles POST /v1/channels/:id/join
func (h *MembershipHandler) Join(c *gin.Context) {
	channelID := h.verifyChannelTenant(c)
	if channelID == uuid.Nil {
		return // error already written
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
	if !validRoles[req.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be 'member' or 'admin'"})
		return
	}

	err := h.repo.AddMember(c.Request.Context(), channelID, userID, req.Role)
	if err != nil {
		h.logger.Error("failed to join channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Leave handles POST /v1/channels/:id/leave
func (h *MembershipHandler) Leave(c *gin.Context) {
	channelID := h.verifyChannelTenant(c)
	if channelID == uuid.Nil {
		return
	}

	userID := middleware.GetUserID(c)

	err := h.repo.RemoveMember(c.Request.Context(), channelID, userID)
	if err != nil {
		h.logger.Error("failed to leave channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to leave channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// ListMembers handles GET /v1/channels/:id/members
func (h *MembershipHandler) ListMembers(c *gin.Context) {
	channelID := h.verifyChannelTenant(c)
	if channelID == uuid.Nil {
		return
	}

	limit, offset := parsePagination(c, 100, 200)

	members, err := h.repo.ListMembers(c.Request.Context(), channelID, limit, offset)
	if err != nil {
		h.logger.Error("failed to list members", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	c.JSON(http.StatusOK, members)
}
