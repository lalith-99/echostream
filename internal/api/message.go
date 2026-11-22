package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
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

	var key *uuid.UUID
	if headers := c.Request.Header.Values("Idempotency-Key"); len(headers) > 0 {
		value, err := uuid.Parse(headers[0])
		if err != nil || len(headers) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key must be a UUID"})
			return
		}
		key = &value
	}
	msg, created, err := h.svc.Send(c.Request.Context(), tenantID, channelID, userID, req.Content, key)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmptyBody), errors.Is(err, service.ErrBodyTooLong), errors.Is(err, service.ErrInvalidBody):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, repository.ErrMessageKeyConflict):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, repository.ErrMessageForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			h.logger.Error("failed to send message", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send message"})
		}
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, msg)
}

// List returns newest-first history or ascending catch-up after an exclusive cursor.
func (h *MessageHandler) List(c *gin.Context) {
	channelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel ID"})
		return
	}

	page := repository.MessagePage{}
	params := c.Request.URL.Query()
	hasBefore, hasAfter := params.Has("before"), params.Has("after")
	if hasBefore && hasAfter {
		c.JSON(http.StatusBadRequest, gin.H{"error": "use either before or after"})
		return
	}
	if hasBefore {
		page.Before, err = strconv.ParseInt(params.Get("before"), 10, 64)
		if err != nil || page.Before < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'before' parameter"})
			return
		}
	}
	if hasAfter {
		id, err := strconv.ParseInt(params.Get("after"), 10, 64)
		if err != nil || id < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'after' parameter"})
			return
		}
		page.After = &id
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
	page.Limit = limit
	messages, err := h.svc.List(c.Request.Context(), tenantID, channelID, middleware.GetUserID(c), page)
	if errors.Is(err, repository.ErrMessageForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.logger.Error("failed to list messages", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}
