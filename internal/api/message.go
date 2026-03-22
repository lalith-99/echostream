package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/service"
	"go.uber.org/zap"
)

// MessageHandler is a thin HTTP adapter. All business logic lives in service.MessageService.
type MessageHandler struct {
	svc    *service.MessageService
	logger *zap.Logger
}

// NewMessageHandler returns a MessageHandler wired to a MessageService.
func NewMessageHandler(svc *service.MessageService, logger *zap.Logger) *MessageHandler {
	return &MessageHandler{svc: svc, logger: logger}
}

type createMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

// Create handles POST /v1/channels/:id/messages
func (h *MessageHandler) Create(c *gin.Context) {
	var req createMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel ID"})
		return
	}

	userID := middleware.GetUserID(c)
	tenantID := middleware.GetTenantID(c)

	msg, err := h.svc.Send(c.Request.Context(), tenantID, channelID, userID, req.Content)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmptyBody), errors.Is(err, service.ErrBodyTooLong):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrNotMember):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			h.logger.Error("failed to send message", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send message"})
		}
		return
	}

	c.JSON(http.StatusCreated, msg)
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
	if before < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'before' parameter"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		limit, err = strconv.Atoi(l)
		if err != nil || limit < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'limit' parameter"})
			return
		}
	}

	tenantID := middleware.GetTenantID(c)
	messages, err := h.svc.List(c.Request.Context(), tenantID, channelID, before, limit)
	if err != nil {
		h.logger.Error("failed to list messages", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}
