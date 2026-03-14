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
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  wsBufferSize,
	WriteBufferSize: wsBufferSize,
	CheckOrigin: func(r *http.Request) bool {
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token query parameter"})
		return
	}

	claims, err := auth.ParseToken(tokenString, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
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
