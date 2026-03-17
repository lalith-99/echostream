package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"github.com/lalith-99/echostream/internal/websocket"
	"go.uber.org/zap"
)

// EventPublisher publishes events to a pub/sub channel (e.g., Redis).
type EventPublisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

// MessageHandler handles message operations.
type MessageHandler struct {
	repo      repository.MessageRepository
	publisher EventPublisher
	logger    *zap.Logger
}

// NewMessageHandler returns a MessageHandler.
func NewMessageHandler(repo repository.MessageRepository, publisher EventPublisher, logger *zap.Logger) *MessageHandler {
	return &MessageHandler{repo: repo, publisher: publisher, logger: logger}
}

type createMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

// Create handles POST	/v1/channels/:id/messages
func (h *MessageHandler) Create(c *gin.Context) {
	var req createMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID := middleware.GetUserID(c)
	tenantID := middleware.GetTenantID(c)
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel ID"})
		return
	}
	ch, err := h.repo.Create(c.Request.Context(), tenantID, channelID, userID, req.Content)
	if err != nil {
		h.logger.Error("failed to create message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create message"})
		return
	}

	// Fan out via Redis so all connected WS clients see it.
	if h.publisher != nil {
		event := websocket.OutboundEvent{
			Type:      "message",
			ChannelID: channelID.String(),
			Message:   ch,
		}
		data, _ := json.Marshal(event)
		if err := h.publisher.Publish(c.Request.Context(), "ch:"+channelID.String(), data); err != nil {
			h.logger.Error("failed to publish message event", zap.Error(err))
		}
	}

	c.JSON(http.StatusCreated, ch)
}

// List handles GET /v1/channels/:id/messages?before=123&limit=50
func (h *MessageHandler) List(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel ID"})
		return
	}

	var before int64
	if b := c.Query("before"); b != "" {
		before, err = strconv.ParseInt(b, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'before' parameter"})
			return
		}
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		limit, err = strconv.Atoi(l)
		if err != nil || limit < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'limit' parameter"})
			return
		}
		if limit > 100 {
			limit = 100
		}
	}
	if before < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'before' parameter"})
		return
	}

	tenantID := middleware.GetTenantID(c)
	messages, err := h.repo.ListByChannel(c.Request.Context(), tenantID, channelID, before, limit)
	if err != nil {
		h.logger.Error("failed to list messages", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}
