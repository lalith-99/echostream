package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

type ChannelHandler struct {
	repo       repository.ChannelRepository
	membership repository.MembershipRepository
	logger     *zap.Logger
}

// NewChannelHandler returns a ChannelHandler.
func NewChannelHandler(repo repository.ChannelRepository, membership repository.MembershipRepository, logger *zap.Logger) *ChannelHandler {
	return &ChannelHandler{repo: repo, membership: membership, logger: logger}
}

type createChannelRequest struct {
	Name      string `json:"name" binding:"required"`
	IsPrivate bool   `json:"is_private"`
}

const maxChannelNameLen = 80

// Create handles POST /v1/channels
func (h *ChannelHandler) Create(c *gin.Context) {
	var req createChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Name) > maxChannelNameLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name must be 80 characters or less"})
		return
	}

	tenantID := middleware.GetTenantID(c)

	ch, err := h.repo.Create(c.Request.Context(), tenantID, req.Name, req.IsPrivate)
	if err != nil {
		h.logger.Error("failed to create channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel"})
		return
	}

	c.JSON(http.StatusCreated, ch)

	// Auto-add the creator as the first admin member.
	// This runs after the response is sent (best-effort). If it fails, the
	// creator can still join manually, but we log the error.
	if err := h.membership.AddMember(c.Request.Context(), ch.ID, middleware.GetUserID(c), "admin"); err != nil {
		h.logger.Error("failed to add creator as admin", zap.Error(err))
	}
}

// List handles GET /v1/channels?limit=50&offset=0
func (h *ChannelHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	limit, offset := parsePagination(c, 50, 100)

	channels, err := h.repo.ListByTenant(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		h.logger.Error("failed to list channels", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list channels"})
		return
	}

	c.JSON(http.StatusOK, channels)
}

// GetByID handles GET /v1/channels/:id
func (h *ChannelHandler) GetByID(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return
	}

	ch, err := h.repo.GetByID(c.Request.Context(), tenantID, channelID)
	if err != nil {
		h.logger.Error("failed to get channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get channel"})
		return
	}

	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}

	c.JSON(http.StatusOK, ch)
}
