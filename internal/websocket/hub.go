package websocket

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/presence"
	"go.uber.org/zap"
)

const maxSubscriptions = 64

type command struct {
	client  *Client
	channel uuid.UUID
	kind    string
}

type frame struct {
	channel uuid.UUID // nonzero frames require a fresh membership check before delivery
	data    []byte
}

type Hub struct {
	channels        map[uuid.UUID]map[*Client]struct{}
	clients         map[*Client]map[uuid.UUID]struct{}
	register        chan *Client
	unregister      chan *Client
	commands        chan command
	broadcast       chan frame
	resync          chan struct{}
	stop            chan struct{}
	done            chan struct{}
	stopOnce        sync.Once
	active          atomic.Int64
	presence        *presence.Tracker
	presenceWorkers sync.WaitGroup
	logger          *zap.Logger
}

func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		logger:     logger,
		channels:   make(map[uuid.UUID]map[*Client]struct{}),
		clients:    make(map[*Client]map[uuid.UUID]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		commands:   make(chan command),
		broadcast:  make(chan frame, 256),
		resync:     make(chan struct{}, 1),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Call before Run.
func (h *Hub) SetPresenceTracker(t *presence.Tracker) { h.presence = t }
func (h *Hub) ConnectionCount() int64                 { return h.active.Load() }

func (h *Hub) Register(c *Client) bool {
	select {
	case <-h.stop:
		return false
	case h.register <- c:
		return true
	}
}

func (h *Hub) Unregister(c *Client) {
	select {
	case <-h.stop:
	case h.unregister <- c:
	}
}

func (h *Hub) apply(c *Client, kind string, channel uuid.UUID) {
	select {
	case <-h.stop:
	case <-c.ctx.Done():
	case h.commands <- command{client: c, kind: kind, channel: channel}:
	}
}

func (h *Hub) Broadcast(channel uuid.UUID, data []byte) {
	select {
	case <-h.stop:
	case h.broadcast <- frame{channel: channel, data: data}:
	}
}

// A Redis reconnect may have missed publications. Every open subscriber
// needs to fetch its durable history, even when no newer frame arrives.
func (h *Hub) Resync() {
	select {
	case h.resync <- struct{}{}:
	default:
	}
}

func (h *Hub) Shutdown() {
	h.stopOnce.Do(func() { close(h.stop) })
	<-h.done
}

func (h *Hub) Run() {
	defer close(h.done)
	for {
		select {
		case <-h.stop:
			h.logger.Info("closing websocket sessions", zap.Int("clients", len(h.clients)))
			for c := range h.clients {
				c.stop()
			}
			for c := range h.clients {
				if c.pumpsDone != nil {
					<-c.pumpsDone
				}
			}
			h.active.Store(0)
			h.presenceWorkers.Wait()
			return
		case c := <-h.register:
			h.clients[c] = make(map[uuid.UUID]struct{})
			h.active.Add(1)
			if h.presence != nil {
				h.presenceWorkers.Add(1)
				go func() { defer h.presenceWorkers.Done(); h.presence.Track(c.ctx, c.userID) }()
			}
		case c := <-h.unregister:
			if subscribed, ok := h.clients[c]; ok {
				c.stop()
				for channel := range subscribed {
					h.remove(c, channel)
				}
				delete(h.clients, c)
				h.active.Add(-1)
			}
		case cmd := <-h.commands:
			h.process(cmd)
		case f := <-h.broadcast:
			for c := range h.channels[f.channel] {
				c.enqueue(f)
			}
		case <-h.resync:
			for channel, clients := range h.channels {
				f := eventFrame("resync", channel, uuid.Nil, true)
				for c := range clients {
					c.enqueue(f)
				}
			}
		}
	}
}

func eventFrame(kind string, channel, user uuid.UUID, check bool) frame {
	ev := OutboundEvent{Type: kind, ChannelID: channel.String()}
	if user != uuid.Nil {
		ev.UserID = user.String()
	}
	payload, _ := json.Marshal(ev) // fields populated above are all strings
	f := frame{data: payload}
	if check {
		f.channel = channel
	}
	return f
}

func (h *Hub) process(cmd command) {
	c, id := cmd.client, cmd.channel
	subs, registered := h.clients[c]
	if !registered {
		return
	}
	_, subscribed := subs[id]
	switch cmd.kind {
	case "subscribe":
		if !subscribed && len(subs) >= maxSubscriptions {
			c.sendError("channel subscription limit reached")
			return
		}
		if !subscribed {
			if h.channels[id] == nil {
				h.channels[id] = make(map[*Client]struct{})
			}
			subs[id] = struct{}{}
			h.channels[id][c] = struct{}{}
		}
		c.enqueue(eventFrame("subscribed", id, uuid.Nil, false))
		if !subscribed {
			h.notifyPeers(c, id, "presence_refresh")
		}
	case "unsubscribe":
		h.remove(c, id)
		c.enqueue(eventFrame("unsubscribed", id, uuid.Nil, false))
	case "revoked":
		if subscribed {
			h.remove(c, id)
			c.enqueue(eventFrame("unsubscribed", id, uuid.Nil, false))
		}
	case "typing":
		if subscribed {
			h.notifyPeers(c, id, "typing")
		}
	}
}

func (h *Hub) notifyPeers(sender *Client, channel uuid.UUID, kind string) {
	f := eventFrame(kind, channel, sender.userID, true)
	for peer := range h.channels[channel] {
		if peer.userID != sender.userID {
			peer.enqueue(f)
		}
	}
}

func (h *Hub) remove(c *Client, id uuid.UUID) {
	if _, ok := h.clients[c][id]; !ok {
		return
	}
	delete(h.clients[c], id)
	delete(h.channels[id], c)
	if len(h.channels[id]) == 0 {
		delete(h.channels, id)
	} else {
		h.notifyPeers(c, id, "presence_refresh")
	}
}
