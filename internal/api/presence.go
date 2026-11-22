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
// Batches presence lookups for a bounded page of members.
func (h *PresenceHandler) GetChannelPresence(c *gin.Context) {
	ch := visibleChannel(c, h.channels, h.logger)
	if ch == nil {
		return
	}
	limit, offset := parsePagination(c, 1000, 1000)
	members, err := h.membership.ListMembers(c.Request.Context(), middleware.GetTenantID(c), ch.ID, middleware.GetUserID(c), limit, offset)
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

	if statuses == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "presence temporarily unavailable"})
		return
	}

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
