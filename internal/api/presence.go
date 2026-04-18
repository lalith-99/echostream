package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/presence"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

// PresenceChecker abstracts the presence tracker so the handler is unit-testable.
type PresenceChecker interface {
	BulkStatus(ctx context.Context, userIDs []uuid.UUID) map[uuid.UUID]presence.Status
}

// PresenceHandler serves channel presence queries.
type PresenceHandler struct {
	channels   repository.ChannelRepository
	membership repository.MembershipRepository
	tracker    PresenceChecker
	logger     *zap.Logger
}

// NewPresenceHandler returns a handler for presence endpoints.
func NewPresenceHandler(
	channels repository.ChannelRepository,
	membership repository.MembershipRepository,
	tracker PresenceChecker,
	logger *zap.Logger,
) *PresenceHandler {
	return &PresenceHandler{
		channels:   channels,
		membership: membership,
		tracker:    tracker,
		logger:     logger,
	}
}

// memberPresence is the JSON shape returned for each user.
type memberPresence struct {
	UserID uuid.UUID       `json:"user_id"`
	Role   string          `json:"role"`
	Status presence.Status `json:"status"` // "online" or "offline"
}

// GetChannelPresence handles GET /v1/channels/:id/presence
//
// Returns an array of members with their online/offline status.
// Uses Redis MGET for a single round-trip regardless of member count.
func (h *PresenceHandler) GetChannelPresence(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	tenantID := middleware.GetTenantID(c)

	// Verify the channel exists and belongs to the caller's tenant.
	ch, err := h.channels.GetByID(c.Request.Context(), tenantID, channelID)
	if err != nil {
		h.logger.Error("failed to get channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}

	// Fetch all members (use a generous limit — presence is lightweight).
	members, err := h.membership.ListMembers(c.Request.Context(), channelID, 1000, 0)
	if err != nil {
		h.logger.Error("failed to list members", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if len(members) == 0 {
		c.JSON(http.StatusOK, []memberPresence{})
		return
	}

	// Collect user IDs for bulk presence query.
	userIDs := make([]uuid.UUID, len(members))
	for i, m := range members {
		userIDs[i] = m.UserID
	}

	statuses := h.tracker.BulkStatus(c.Request.Context(), userIDs)

	result := make([]memberPresence, len(members))
	for i, m := range members {
		status := presence.Offline
		if s, ok := statuses[m.UserID]; ok {
			status = s
		}
		result[i] = memberPresence{
			UserID: m.UserID,
			Role:   m.Role,
			Status: status,
		}
	}

	c.JSON(http.StatusOK, result)
}
