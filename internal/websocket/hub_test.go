package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func startHub(t *testing.T) *Hub {
	t.Helper()
	h := NewHub(zap.NewNop())
	go h.Run()
	t.Cleanup(h.Shutdown)
	return h
}

func testClient(t *testing.T, h *Hub) *Client {
	t.Helper()
	c := NewClient(h, nil, uuid.New(), time.Now().Add(time.Hour), func(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return true, nil }, zap.NewNop())
	if !h.Register(c) {
		t.Fatal("registration refused")
	}
	return c
}

func next(t *testing.T, c *Client, kind string) frame {
	t.Helper()
	select {
	case f := <-c.send:
		var event OutboundEvent
		if err := json.Unmarshal(f.data, &event); err != nil || event.Type != kind {
			t.Fatalf("expected %s, got %s, err=%v", kind, f.data, err)
		}
		return f
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s", kind)
		return frame{}
	}
}

func subscribe(t *testing.T, c *Client, id uuid.UUID) {
	t.Helper()
	c.handleMessage(InboundMessage{Type: "subscribe", ChannelID: id.String()})
	next(t, c, "subscribed")
}

func TestHubRoutesOnlySubscribedPeersAndSignalsReconnect(t *testing.T) {
	h := startHub(t)
	a, b, other := testClient(t, h), testClient(t, h), testClient(t, h)
	id := uuid.New()
	subscribe(t, a, id)
	subscribe(t, b, id)
	next(t, a, "presence_refresh")
	if h.ConnectionCount() != 3 {
		t.Fatalf("active connections = %d", h.ConnectionCount())
	}
	other.handleMessage(InboundMessage{Type: "typing", ChannelID: id.String()})
	a.handleMessage(InboundMessage{Type: "typing", ChannelID: id.String()})
	f := next(t, b, "typing")
	if f.channel != id {
		t.Fatal("typing event missing delivery membership check")
	}
	var ev OutboundEvent
	if err := json.Unmarshal(f.data, &ev); err != nil || ev.UserID != a.userID.String() {
		t.Fatalf("typing event from wrong sender: %s", f.data)
	}

	h.Broadcast(id, []byte(`{"type":"message"}`))
	for _, c := range []*Client{a, b} {
		if f := next(t, c, "message"); f.channel != id {
			t.Fatal("message missing delivery membership check")
		}
	}
	h.Resync()
	for _, c := range []*Client{a, b} {
		next(t, c, "resync")
	}
	if len(other.send) != 0 {
		t.Fatal("unexpected delivery to unsubscribed client")
	}
	b.handleMessage(InboundMessage{Type: "unsubscribe", ChannelID: id.String()})
	next(t, b, "unsubscribed")
	next(t, a, "presence_refresh")
	h.Broadcast(id, []byte(`{"type":"message"}`))
	next(t, a, "message")
	if len(b.send) != 0 {
		t.Fatal("delivery after unsubscribe")
	}

	h.Unregister(b)
	h.Resync()
	next(t, a, "resync")
	if h.ConnectionCount() != 2 {
		t.Fatalf("active connections after unregister = %d", h.ConnectionCount())
	}
}

func TestHubSubscriptionLimit(t *testing.T) {
	h := startHub(t)
	c := testClient(t, h)
	for i := 0; i < maxSubscriptions; i++ {
		subscribe(t, c, uuid.New())
	}
	c.handleMessage(InboundMessage{Type: "subscribe", ChannelID: uuid.NewString()})
	next(t, c, "error")
}

func TestFullQueueClosesSessionForRecovery(t *testing.T) {
	h := startHub(t)
	c := testClient(t, h)
	for i := 0; i <= sendBufSize; i++ {
		c.enqueue(frame{data: []byte("x")})
	}
	if c.ctx.Err() == nil {
		t.Fatal("full queue did not close client")
	}
}

func TestShutdownClosesClientsAndUnblocksCallers(t *testing.T) {
	h := startHub(t)
	c := testClient(t, h)
	h.Shutdown()
	if c.ctx.Err() == nil || h.ConnectionCount() != 0 {
		t.Fatal("hub did not drain")
	}
	if h.Register(c) {
		t.Fatal("registration after shutdown")
	}
	h.Broadcast(uuid.New(), []byte("closed"))
	h.Unregister(c)
}
