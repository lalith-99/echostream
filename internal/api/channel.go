package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/models"
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

const maxChannelNameLen = 80

// Create handles POST /v1/channels
func (h *ChannelHandler) Create(c *gin.Context) {
	var req createChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > maxChannelNameLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel name must be between 1 and 80 characters"})
		return
	}

	tenantID := middleware.GetTenantID(c)

	ch, err := h.repo.Create(c.Request.Context(), tenantID, middleware.GetUserID(c), req.Name, req.IsPrivate)
	if errors.Is(err, repository.ErrMembershipUnavailable) {
		c.JSON(http.StatusForbidden, gin.H{"error": "workspace membership required"})
		return
	}
	if err != nil {
		h.logger.Error("failed to create channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel"})
		return
	}

	c.JSON(http.StatusCreated, ch)
}

// List handles GET /v1/channels?limit=50&offset=0
func (h *ChannelHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)

	limit, offset := parsePagination(c, 50, 100)

	channels, err := h.repo.ListByTenant(c.Request.Context(), tenantID, userID, limit, offset)
	if err != nil {
		h.logger.Error("failed to list channels", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list channels"})
		return
	}

	c.JSON(http.StatusOK, channels)
}

// GetByID handles GET /v1/channels/:id
func (h *ChannelHandler) GetByID(c *gin.Context) {
	if ch := visibleChannel(c, h.repo, h.logger); ch != nil {
		c.JSON(http.StatusOK, ch)
	}
}

// visibleChannel resolves a channel for its viewer or writes the appropriate response.
func visibleChannel(c *gin.Context, channels repository.ChannelRepository, logger *zap.Logger) *models.Channel {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
		return nil
	}

	ch, err := channels.GetByID(c.Request.Context(), middleware.GetTenantID(c), channelID, middleware.GetUserID(c))
	if err != nil {
		logger.Error("failed to get channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get channel"})
		return nil
	}
	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
	}
	return ch
}
