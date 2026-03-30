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
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10 // 54s
	maxMessageSize = 4096
	sendBufSize    = 256
)

// MembershipChecker verifies whether a user belongs to a channel.
// Injected from the api layer so the websocket package doesn't import repository.
type MembershipChecker func(ctx context.Context, channelID, userID uuid.UUID) (bool, error)

type Client struct {
	hub             *Hub
	conn            *gorillaws.Conn
	send            chan []byte
	userID          uuid.UUID
	tenantID        uuid.UUID
	checkMembership MembershipChecker // nil = skip check (backwards compat for tests)
	logger          *zap.Logger
}

// NewClient creates a websocket client bound to a hub.
func NewClient(hub *Hub, conn *gorillaws.Conn, userID, tenantID uuid.UUID, checker MembershipChecker, logger *zap.Logger) *Client {
	return &Client{
		hub:             hub,
		conn:            conn,
		send:            make(chan []byte, sendBufSize),
		userID:          userID,
		tenantID:        tenantID,
		checkMembership: checker,
		logger:          logger,
	}
}

// Send queues data for writing to the WebSocket. Drops if buffer is full.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}

// ReadPump reads messages from the WebSocket and dispatches them to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if gorillaws.IsUnexpectedCloseError(err, gorillaws.CloseGoingAway, gorillaws.CloseNormalClosure) {
				c.logger.Warn("ws read error", zap.Error(err))
			}
			return
		}

		var msg InboundMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.logger.Debug("invalid ws message", zap.Error(err))
			c.sendError("invalid message format")
			continue
		}
		c.handleMessage(msg)
	}
}

// WritePump pumps messages from the send channel to the WebSocket.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case data, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(gorillaws.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(gorillaws.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(gorillaws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(msg InboundMessage) {
	switch msg.Type {
	case "subscribe":
		channelID, err := uuid.Parse(msg.ChannelID)
		if err != nil {
			c.sendError("invalid channel_id")
			return
		}
		// Verify caller is a member before subscribing to real-time events
		if c.checkMembership != nil {
			ok, err := c.checkMembership(context.Background(), channelID, c.userID)
			if err != nil {
				c.logger.Error("membership check failed", zap.Error(err))
				c.sendError("internal error")
				return
			}
			if !ok {
				c.sendError("not a member of this channel")
				return
			}
		}
		c.hub.subscribeCh <- &subscription{client: c, channelID: channelID}

	case "unsubscribe":
		channelID, err := uuid.Parse(msg.ChannelID)
		if err != nil {
			c.sendError("invalid channel_id")
			return
		}
		c.hub.unsubscribeCh <- &subscription{client: c, channelID: channelID}

	case "typing":
		channelID, err := uuid.Parse(msg.ChannelID)
		if err != nil {
			c.sendError("invalid channel_id")
			return
		}
		c.hub.typingCh <- &typingEvent{channelID: channelID, userID: c.userID}

	default:
		c.sendError("unknown message type: " + msg.Type)
	}
}

func (c *Client) sendError(msg string) {
	data, err := json.Marshal(OutboundEvent{Type: "error", Error: msg})
	if err != nil {
		c.logger.Error("failed to marshal error event", zap.Error(err))
		return
	}
	c.Send(data)
}
