package websocket

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/presence"
	"go.uber.org/zap"
)

type subscription struct {
	client    *Client
	channelID uuid.UUID
}

type typingEvent struct {
	channelID uuid.UUID
	userID    uuid.UUID
}

type broadcastMessage struct {
	channelID uuid.UUID
	data      []byte
}

// Hub maintains active clients and routes messages between them.
// All state is managed in the Run goroutine — no locks needed.
type Hub struct {
	// channelID → set of subscribed clients
	channels map[uuid.UUID]map[*Client]struct{}
	// client → set of channels they're in
	clientChannels map[*Client]map[uuid.UUID]struct{}

	register      chan *Client
	unregister    chan *Client
	subscribeCh   chan *subscription
	unsubscribeCh chan *subscription
	broadcastCh   chan *broadcastMessage
	typingCh      chan *typingEvent
	shutdown      chan struct{}

	// Called when a channel gets its first local subscriber.
	onChannelActive func(channelID uuid.UUID)
	// Called when a channel loses its last local subscriber.
	onChannelInactive func(channelID uuid.UUID)

	presence  *presence.Tracker              // nil until SetPresenceTracker is called
	userConns map[uuid.UUID]int              // open WS conns per userID
	cancelKA  map[*Client]context.CancelFunc // per-client keepalive cancel

	logger *zap.Logger
}

// NewHub creates a Hub for managing websocket clients.
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		channels:       make(map[uuid.UUID]map[*Client]struct{}),
		clientChannels: make(map[*Client]map[uuid.UUID]struct{}),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		subscribeCh:    make(chan *subscription),
		unsubscribeCh:  make(chan *subscription),
		broadcastCh:    make(chan *broadcastMessage, 256),
		typingCh:       make(chan *typingEvent, 256),
		shutdown:       make(chan struct{}),
		userConns:      make(map[uuid.UUID]int),
		cancelKA:       make(map[*Client]context.CancelFunc),
		logger:         logger,
	}
}

// SetChannelCallbacks wires the hub to Redis pub/sub.
func (h *Hub) SetChannelCallbacks(onActive, onInactive func(uuid.UUID)) {
	h.onChannelActive = onActive
	h.onChannelInactive = onInactive
}

// SetPresenceTracker enables online/offline tracking via Redis.
func (h *Hub) SetPresenceTracker(t *presence.Tracker) {
	h.presence = t
}

// Register queues a new client for the hub to track.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Broadcast sends data to all local clients in a channel.
// Safe to call from any goroutine (e.g., the Redis listener).
func (h *Hub) Broadcast(channelID uuid.UUID, data []byte) {
	h.broadcastCh <- &broadcastMessage{channelID: channelID, data: data}
}

// Shutdown signals the hub to stop processing events.
func (h *Hub) Shutdown() {
	close(h.shutdown)
}

// Run is the hub's main event loop. Must be started as a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case <-h.shutdown:
			h.logger.Info("hub shutting down")
			return
		case client := <-h.register:
			h.clientChannels[client] = make(map[uuid.UUID]struct{})
			h.userConns[client.userID]++

			// Start presence tracking for this connection
			if h.presence != nil {
				ctx, cancel := context.WithCancel(context.Background())
				h.cancelKA[client] = cancel
				h.presence.SetOnline(ctx, client.userID)
				go h.presence.KeepAlive(ctx, client.userID)
			}

			h.logger.Debug("client connected",
				zap.String("user_id", client.userID.String()),
			)

		case client := <-h.unregister:
			if channels, ok := h.clientChannels[client]; ok {
				for chID := range channels {
					h.removeFromChannel(client, chID)
				}
				delete(h.clientChannels, client)
				close(client.send)
			}

			// Stop this client's keepalive goroutine
			if cancel, ok := h.cancelKA[client]; ok {
				cancel()
				delete(h.cancelKA, client)
			}

			// Only set offline when last connection for this user closes
			h.userConns[client.userID]--
			if h.userConns[client.userID] <= 0 {
				delete(h.userConns, client.userID)
				if h.presence != nil {
					h.presence.SetOffline(context.Background(), client.userID)
				}
			}

			h.logger.Debug("client disconnected",
				zap.String("user_id", client.userID.String()),
			)

		case sub := <-h.subscribeCh:
			h.addToChannel(sub.client, sub.channelID)
			data, err := json.Marshal(OutboundEvent{Type: "subscribed", ChannelID: sub.channelID.String()})
			if err != nil {
				h.logger.Error("failed to marshal subscribed event", zap.Error(err))
			} else {
				sub.client.Send(data)
			}
			h.logger.Debug("client subscribed to channel",
				zap.String("user_id", sub.client.userID.String()),
				zap.String("channel_id", sub.channelID.String()),
			)

		case sub := <-h.unsubscribeCh:
			h.removeFromChannel(sub.client, sub.channelID)
			data, err := json.Marshal(OutboundEvent{Type: "unsubscribed", ChannelID: sub.channelID.String()})
			if err != nil {
				h.logger.Error("failed to marshal unsubscribed event", zap.Error(err))
			} else {
				sub.client.Send(data)
			}

		case msg := <-h.broadcastCh:
			if clients, ok := h.channels[msg.channelID]; ok {
				for client := range clients {
					client.Send(msg.data)
				}
			}

		case ev := <-h.typingCh:
			if clients, ok := h.channels[ev.channelID]; ok {
				event := OutboundEvent{
					Type:      "typing",
					ChannelID: ev.channelID.String(),
					UserID:    ev.userID.String(),
				}
				data, err := json.Marshal(event)
				if err != nil {
					h.logger.Error("failed to marshal typing event", zap.Error(err))
					continue
				}
				for client := range clients {
					if client.userID != ev.userID {
						client.Send(data)
					}
				}
			}
		}
	}
}

func (h *Hub) addToChannel(client *Client, channelID uuid.UUID) {
	if _, ok := h.channels[channelID]; !ok {
		h.channels[channelID] = make(map[*Client]struct{})
		if h.onChannelActive != nil {
			h.onChannelActive(channelID)
		}
	}
	h.channels[channelID][client] = struct{}{}
	h.clientChannels[client][channelID] = struct{}{}
}

func (h *Hub) removeFromChannel(client *Client, channelID uuid.UUID) {
	if clients, ok := h.channels[channelID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.channels, channelID)
			if h.onChannelInactive != nil {
				h.onChannelInactive(channelID)
			}
		}
	}
	if channels, ok := h.clientChannels[client]; ok {
		delete(channels, channelID)
	}
}
