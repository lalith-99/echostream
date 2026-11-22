package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
	"github.com/lalith-99/echostream/internal/auth"
	"github.com/lalith-99/echostream/internal/repository"
	"github.com/lalith-99/echostream/internal/websocket"
	"go.uber.org/zap"
)

const (
	wsProtocol        = "echostream.v1"
	maxSockets        = 1024
	maxSocketsPerUser = 8
)

type WSHandler struct {
	hub            *websocket.Hub
	membershipRepo repository.MembershipRepository
	jwtSecret      string
	logger         *zap.Logger
	upgrader       gorillaws.Upgrader
	mu             sync.Mutex
	perUser        map[uuid.UUID]int
	active         int
}

func NewWSHandler(hub *websocket.Hub, membershipRepo repository.MembershipRepository, jwtSecret string, logger *zap.Logger, allowedOrigins ...string) *WSHandler {
	return &WSHandler{
		hub:            hub,
		membershipRepo: membershipRepo,
		jwtSecret:      jwtSecret,
		logger:         logger,
		perUser:        make(map[uuid.UUID]int),
		upgrader: gorillaws.Upgrader{
			ReadBufferSize:   1024,
			WriteBufferSize:  1024,
			HandshakeTimeout: 5 * time.Second,
			Subprotocols:     []string{wsProtocol},
			CheckOrigin: func(r *http.Request) bool {
				values := r.Header.Values("Origin")
				return len(values) == 0 || (len(values) == 1 && values[0] != "" && slices.Contains(allowedOrigins, values[0]))
			},
		},
	}
}

// Browsers present a bearer token as a second offered subprotocol. The
// server selects only the protocol name; the credential stays out of URLs.
func wsBearer(r *http.Request) string {
	if authz := strings.Fields(r.Header.Get("Authorization")); len(authz) == 2 && strings.EqualFold(authz[0], "Bearer") {
		return authz[1]
	}
	protocols := gorillaws.Subprotocols(r)
	if len(protocols) == 2 && protocols[0] == wsProtocol {
		if token, ok := strings.CutPrefix(protocols[1], "bearer."); ok {
			return token
		}
	}
	return ""
}

func (h *WSHandler) acquire(user uuid.UUID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active >= maxSockets || h.perUser[user] >= maxSocketsPerUser {
		return false
	}
	h.active++
	h.perUser[user]++
	return true
}

func (h *WSHandler) release(user uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.active--
	h.perUser[user]--
	if h.perUser[user] == 0 {
		delete(h.perUser, user)
	}
}

func (h *WSHandler) HandleWS(c *gin.Context) {
	claims, err := auth.ParseToken(wsBearer(c.Request), h.jwtSecret)
	if err != nil || claims.ExpiresAt == nil || claims.UserID == uuid.Nil || claims.TenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "valid bearer token required"})
		return
	}
	if !h.acquire(claims.UserID) {
		c.Header("Retry-After", "5")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "connection limit reached"})
		return
	}
	defer h.release(claims.UserID)
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	check := func(ctx context.Context, channel, user uuid.UUID) (bool, error) {
		return h.membershipRepo.IsMember(ctx, claims.TenantID, channel, user)
	}
	client := websocket.NewClient(h.hub, conn, claims.UserID, claims.ExpiresAt.Time, check, h.logger)
	client.Serve()
}
