package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/models"
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
// Returns the full Channel model so callers can inspect fields like IsPrivate.
// On error it writes the HTTP response and returns nil.
func (h *MembershipHandler) verifyChannelTenant(c *gin.Context) *models.Channel {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return nil
	}

	tenantID := middleware.GetTenantID(c)
	ch, err := h.channels.GetByID(c.Request.Context(), tenantID, channelID)
	if err != nil {
		h.logger.Error("failed to verify channel tenant", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return nil
	}
	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return nil
	}
	return ch
}

// Join handles POST /v1/channels/:id/join
//
// Public channels: anyone in the tenant can join.
// Private channels: the caller must already be a member (i.e., they were invited).
// This allows the invite flow to add the user first, then the user "accepts"
// by calling join (or the invite auto-adds them and join is a no-op via ON CONFLICT).
func (h *MembershipHandler) Join(c *gin.Context) {
	ch := h.verifyChannelTenant(c)
	if ch == nil {
		return // error already written
	}

	userID := middleware.GetUserID(c)

	// Private channels require an existing membership (set by Invite).
	if ch.IsPrivate {
		already, err := h.repo.IsMember(c.Request.Context(), ch.ID, userID)
		if err != nil {
			h.logger.Error("failed to check membership", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if !already {
			c.JSON(http.StatusForbidden, gin.H{"error": "private channel — invite required"})
			return
		}
	}

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

	err := h.repo.AddMember(c.Request.Context(), ch.ID, userID, req.Role)
	if err != nil {
		h.logger.Error("failed to join channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join channel"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Leave handles POST /v1/channels/:id/leave
func (h *MembershipHandler) Leave(c *gin.Context) {
	ch := h.verifyChannelTenant(c)
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
	ch := h.verifyChannelTenant(c)
	if ch == nil {
		return
	}

	callerID := middleware.GetUserID(c)

	// Only existing members can invite others.
	isMember, err := h.repo.IsMember(c.Request.Context(), ch.ID, callerID)
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

	// AddMember is idempotent (ON CONFLICT DO NOTHING), so re-inviting is harmless.
	if err := h.repo.AddMember(c.Request.Context(), ch.ID, targetID, "member"); err != nil {
		h.logger.Error("failed to invite user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to invite user"})
		return
	}

	c.Status(http.StatusNoContent)
}

// ListMembers handles GET /v1/channels/:id/members
func (h *MembershipHandler) ListMembers(c *gin.Context) {
	ch := h.verifyChannelTenant(c)
	if ch == nil {
		return
	}

	limit, offset := parsePagination(c, 100, 200)

	members, err := h.repo.ListMembers(c.Request.Context(), ch.ID, limit, offset)
	if err != nil {
		h.logger.Error("failed to list members", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	c.JSON(http.StatusOK, members)
}
