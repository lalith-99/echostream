package websocket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait            = 5 * time.Second
	pongWait             = 60 * time.Second
	pingPeriod           = 45 * time.Second
	maxMessageSize       = 4096
	sendBufSize          = 64
	maxCommandsPerSecond = 30
)

type MembershipChecker func(ctx context.Context, channelID, userID uuid.UUID) (bool, error)

type Client struct {
	hub             *Hub
	conn            *gorillaws.Conn
	send            chan frame
	ctx             context.Context
	cancel          context.CancelFunc
	pumpsDone       chan struct{}
	userID          uuid.UUID
	expires         time.Time
	checkMembership MembershipChecker
	logger          *zap.Logger
}

func NewClient(hub *Hub, conn *gorillaws.Conn, userID uuid.UUID, expires time.Time, checker MembershipChecker, logger *zap.Logger) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		hub: hub, conn: conn, send: make(chan frame, sendBufSize),
		ctx: ctx, cancel: cancel, userID: userID, expires: expires,
		checkMembership: checker, logger: logger,
	}
}

func (c *Client) stop() {
	c.cancel()
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *Client) closeWith(code int, reason string) {
	_ = c.conn.WriteControl(gorillaws.CloseMessage, gorillaws.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	c.stop()
}

// Disconnect a slow socket so its next subscription can recover from history.
// Silently dropping a queued message would leave a connected client with a gap.
func (c *Client) enqueue(f frame) {
	select {
	case <-c.ctx.Done():
	case c.send <- f:
	default:
		c.stop()
	}
}

func (c *Client) Serve() {
	c.pumpsDone = make(chan struct{})
	defer close(c.pumpsDone)
	if !c.hub.Register(c) {
		c.stop()
		return
	}
	defer c.hub.Unregister(c)
	expiry := time.AfterFunc(time.Until(c.expires), func() { c.closeWith(gorillaws.ClosePolicyViolation, "session expired") })
	defer expiry.Stop()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.writePump()
		c.stop()
	}()
	c.readPump()
	c.stop()
	<-writerDone
}

func (c *Client) member(id uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	return c.checkMembership(ctx, id, c.userID)
}

func (c *Client) readPump() {
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(pongWait)) })
	window := time.Now()
	count := 0
	for {
		kind, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if time.Since(window) >= time.Second {
			window = time.Now()
			count = 0
		}
		count++
		if count > maxCommandsPerSecond {
			c.closeWith(gorillaws.ClosePolicyViolation, "command rate exceeded")
			return
		}
		var msg InboundMessage
		if kind != gorillaws.TextMessage || json.Unmarshal(raw, &msg) != nil {
			c.sendError("invalid message format")
			continue
		}
		c.handleMessage(msg)
	}
}

func (c *Client) handleMessage(msg InboundMessage) {
	id, err := uuid.Parse(msg.ChannelID)
	if err != nil || id == uuid.Nil {
		c.sendError("invalid channel_id")
		return
	}
	switch msg.Type {
	case "subscribe", "typing":
		allowed, err := c.member(id)
		if err != nil {
			c.sendError("membership unavailable")
			return
		}
		if !allowed {
			c.sendError("not a member of this channel")
			return
		}
	case "unsubscribe":
	default:
		c.sendError("unknown message type")
		return
	}
	c.hub.apply(c, msg.Type, id)
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case f := <-c.send:
			if f.channel != uuid.Nil {
				ok, err := c.member(f.channel)
				if err != nil {
					c.logger.Warn("websocket membership unavailable", zap.Error(err))
					return // reconnect triggers a history refresh when the store recovers
				}
				if !ok {
					c.hub.apply(c, "revoked", f.channel)
					continue
				}
			}
			if !time.Now().Before(c.expires) {
				c.closeWith(gorillaws.ClosePolicyViolation, "session expired")
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if c.conn.WriteMessage(gorillaws.TextMessage, f.data) != nil {
				return
			}
		case <-ticker.C:
			if c.conn.WriteControl(gorillaws.PingMessage, nil, time.Now().Add(writeWait)) != nil {
				return
			}
		}
	}
}

func (c *Client) sendError(msg string) {
	data, _ := json.Marshal(OutboundEvent{Type: "error", Error: msg})
	c.enqueue(frame{data: data})
}
