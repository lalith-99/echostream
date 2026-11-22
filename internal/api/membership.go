package api

import (
	"errors"
	"io"
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
	channels repository.ChannelRepository
	logger   *zap.Logger
}

// NewMembershipHandler returns a MembershipHandler.
func NewMembershipHandler(repo repository.MembershipRepository, channels repository.ChannelRepository, logger *zap.Logger) *MembershipHandler {
	return &MembershipHandler{repo: repo, channels: channels, logger: logger}
}

type joinChannelRequest struct {
	Role string `json:"role"`
}

// Join handles POST /v1/channels/:id/join
//
// Public channels: anyone in the tenant can join.
// Private channels: the caller must already be a member (i.e., they were invited).
// This allows the invite flow to add the user first, then the user "accepts"
// by calling join (or the invite auto-adds them and join is a no-op via ON CONFLICT).
func (h *MembershipHandler) Join(c *gin.Context) {
	ch := visibleChannel(c, h.channels, h.logger)
	if ch == nil {
		return // error already written
	}

	userID := middleware.GetUserID(c)

	// Accept the historical member role and requests with no body.
	var req joinChannelRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid join request"})
			return
		}
	}
	if req.Role != "" && req.Role != "member" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "self-join role must be member"})
		return
	}
	if !h.addMember(c, ch.ID, userID, userID) {
		return
	}
	c.Status(http.StatusNoContent)
}

// Leave handles POST /v1/channels/:id/leave
func (h *MembershipHandler) Leave(c *gin.Context) {
	ch := visibleChannel(c, h.channels, h.logger)
	if ch == nil {
		return
	}

	userID := middleware.GetUserID(c)

	err := h.repo.RemoveMember(c.Request.Context(), ch.ID, userID)
	if err != nil {
		h.logger.Error("failed to leave channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to leave channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Invite handles POST /v1/channels/:id/invite
//
// Adds another user to a channel. Only existing members can invite.
// For private channels, this is the only way to gain access.
// For public channels, it's a convenience (user could also self-join).
type inviteRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

func (h *MembershipHandler) Invite(c *gin.Context) {
	ch := visibleChannel(c, h.channels, h.logger)
	if ch == nil {
		return
	}

	callerID := middleware.GetUserID(c)

	// Only existing members can invite others.
	isMember, err := h.repo.IsMember(c.Request.Context(), middleware.GetTenantID(c), ch.ID, callerID)
	if err != nil {
		h.logger.Error("failed to check caller membership", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "you must be a member to invite others"})
		return
	}

	var req inviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	targetID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	if h.addMember(c, ch.ID, callerID, targetID) {
		c.Status(http.StatusNoContent)
	}
}

func (h *MembershipHandler) addMember(c *gin.Context, channelID, callerID, targetID uuid.UUID) bool {
	err := h.repo.AddMember(c.Request.Context(), middleware.GetTenantID(c), channelID, callerID, targetID)
	if errors.Is(err, repository.ErrMembershipUnavailable) {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel or workspace user not found"})
		return false
	}
	if err != nil {
		h.logger.Error("failed to add channel member", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add channel member"})
		return false
	}
	return true
}

// ListMembers handles GET /v1/channels/:id/members
func (h *MembershipHandler) ListMembers(c *gin.Context) {
	ch := visibleChannel(c, h.channels, h.logger)
	if ch == nil {
		return
	}

	limit, offset := parsePagination(c, 100, 200)

	members, err := h.repo.ListMembers(c.Request.Context(), middleware.GetTenantID(c), ch.ID, middleware.GetUserID(c), limit, offset)
	if err != nil {
		h.logger.Error("failed to list members", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	c.JSON(http.StatusOK, members)
}
