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
	repo   repository.ChannelRepository
	logger *zap.Logger
}

// NewChannelHandler returns a ChannelHandler.
func NewChannelHandler(repo repository.ChannelRepository, logger *zap.Logger) *ChannelHandler {
	return &ChannelHandler{repo: repo, logger: logger}
}

type createChannelRequest struct {
	Name      string `json:"name" binding:"required"`
	IsPrivate bool   `json:"is_private"`
}

// Create handles POST /v1/channels
func (h *ChannelHandler) Create(c *gin.Context) {
	var req createChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
}

// List handles GET /v1/channels
func (h *ChannelHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	channels, err := h.repo.ListByTenant(c.Request.Context(), tenantID)
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
