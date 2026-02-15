package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

type MessageHandler struct {
	repo   repository.MessageRepository
	logger *zap.Logger
}

func NewMessageHandler(repo repository.MessageRepository, logger *zap.Logger) *MessageHandler {
	return &MessageHandler{repo: repo, logger: logger}
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
	c.JSON(http.StatusCreated, ch)
}

// List handles GET /v1/channels/:id/messages?before=123&limit=50
//
// Cursor-based pagination:
//   - "before" = message ID. "Give me messages older than this." 0 = start from latest.
//   - "limit"  = how many to return. Default 50, capped at 100.
func (h *MessageHandler) List(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel ID"})
		return
	}

	// Read query param "before" — defaults to 0 if not provided.
	var before int64
	if b := c.Query("before"); b != "" {
		before, err = strconv.ParseInt(b, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'before' parameter"})
			return
		}
	}

	// Read query param "limit" — defaults to 50, capped at 100.
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

	// Interface: ListByChannel(ctx, tenantID, channelID, before, limit)
	tenantID := middleware.GetTenantID(c)
	messages, err := h.repo.ListByChannel(c.Request.Context(), tenantID, channelID, before, limit)
	if err != nil {
		h.logger.Error("failed to list messages", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}
