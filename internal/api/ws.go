package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
	"github.com/lalith-99/echostream/internal/auth"
	"github.com/lalith-99/echostream/internal/websocket"
	"go.uber.org/zap"
)

const (
	wsBufferSize = 1024
	wsTokenQuery = "token"

	wsErrMissingToken = "missing token query parameter"
	wsErrInvalidToken = "invalid or expired token"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  wsBufferSize,
	WriteBufferSize: wsBufferSize,
	CheckOrigin: func(r *http.Request) bool {
		// Allow localhost and same-origin requests
		// In production, configure allowed origins via environment variable
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Allow requests without Origin header (e.g., native apps)
		}
		// For development, allow all origins. In production, check against allowlist.
		// TODO: Add ALLOWED_ORIGINS env var and validate against it
		return true
	},
}

type WSHandler struct {
	hub       *websocket.Hub
	jwtSecret string
	logger    *zap.Logger
}

// NewWSHandler creates a WebSocket handler.
func NewWSHandler(hub *websocket.Hub, jwtSecret string, logger *zap.Logger) *WSHandler {
	return &WSHandler{hub: hub, jwtSecret: jwtSecret, logger: logger}
}

// HandleWS upgrades HTTP to WebSocket. Auth via ?token=<jwt> query param
// (browsers can't set custom headers on WebSocket connections).
func (h *WSHandler) HandleWS(c *gin.Context) {
	tokenString := c.Query(wsTokenQuery)
	if tokenString == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": wsErrMissingToken})
		return
	}

	claims, err := auth.ParseToken(tokenString, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": wsErrInvalidToken})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("ws upgrade failed", zap.Error(err))
		return
	}

	client := websocket.NewClient(h.hub, conn, claims.UserID, claims.TenantID, h.logger)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
