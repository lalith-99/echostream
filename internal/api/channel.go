package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/repository"
	"go.uber.org/zap"
)

// ChannelHandler holds the dependencies needed to handle channel requests.
//
// Why a struct with methods, not standalone functions?
//   - Each handler method needs access to the repo and logger.
//   - A struct gives us a clean place to hold those dependencies.
//   - In main.go: handler := api.NewChannelHandler(repo, logger)
//     then: v1.POST("/channels", handler.Create)
//
// Why repository.ChannelRepository (interface) and not *postgres.ChannelStore?
//   - The handler doesn't know or care that Postgres is behind the interface.
//   - In tests, you can pass a mock implementation. No DB needed.
type ChannelHandler struct {
	repo   repository.ChannelRepository
	logger *zap.Logger
}

func NewChannelHandler(repo repository.ChannelRepository, logger *zap.Logger) *ChannelHandler {
	return &ChannelHandler{repo: repo, logger: logger}
}

// createChannelRequest is the expected JSON body for POST /v1/channels.
//
// Why a separate struct and not reuse models.Channel?
//   - The API request is NOT the same shape as the DB row.
//   - The client sends: { "name": "general", "is_private": false }
//   - The DB row has: id, tenant_id, created_at — which the client
//     should NEVER control. Reusing the model would let clients set
//     their own ID or created_at. Separate request struct = safety.
//
// binding:"required" — Gin validates this field is present. If missing,
//   ShouldBindJSON returns an error and we send 400 Bad Request.
type createChannelRequest struct {
	Name      string `json:"name" binding:"required"`
	IsPrivate bool   `json:"is_private"`
}

// Create handles POST /v1/channels
func (h *ChannelHandler) Create(c *gin.Context) {
	// Step 1: Parse the JSON body into our request struct.
	// ShouldBindJSON does two things:
	//   1. Unmarshals JSON into the struct
	//   2. Validates binding tags (e.g., "required")
	// If the body is invalid, it returns an error immediately.
	var req createChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Step 2: Get the tenant ID from the JWT (set by AuthMiddleware).
	tenantID := middleware.GetTenantID(c)

	// Step 3: Call the repository to create the channel.
	// c.Request.Context() passes the HTTP request's context to the DB call.
	// If the client disconnects, this context cancels, and the DB query stops.
	ch, err := h.repo.Create(c.Request.Context(), tenantID, req.Name, req.IsPrivate)
	if err != nil {
		h.logger.Error("failed to create channel", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel"})
		return
	}

	// Step 4: Return the created channel.
	// 201 Created — not 200 OK — because a new resource was created.
	// This is a REST convention: POST that creates something returns 201.
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

	// 200 OK with the array. Because our repo returns make([]..., 0),
	// this serializes to [] (empty array) when there are no channels,
	// never null.
	c.JSON(http.StatusOK, channels)
}

// GetByID handles GET /v1/channels/:id
func (h *ChannelHandler) GetByID(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	// c.Param("id") reads the :id path parameter from the URL.
	// e.g., GET /v1/channels/550e8400-... → c.Param("id") = "550e8400-..."
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

	// Remember: repo returns nil, nil when not found.
	// We translate that to a 404.
	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}

	c.JSON(http.StatusOK, ch)
}
