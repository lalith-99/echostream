package redis

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type receivedEvent struct {
	id      uuid.UUID
	payload []byte
}
type captureHub struct {
	target   uuid.UUID
	received chan receivedEvent
}

func (h *captureHub) Broadcast(id uuid.UUID, data []byte) {
	if id == h.target {
		h.received <- receivedEvent{id, data}
	}
}
func (h *captureHub) Resync() {}

func TestSubscribedGatewaysReceiveFirstPublication(t *testing.T) {
	redisURL := os.Getenv("ECHOSTREAM_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("ECHOSTREAM_TEST_REDIS_URL is unset")
	}
	logger := zap.NewNop()
	client, err := NewClient(redisURL, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := uuid.New()
	var hubs []*captureHub
	for range 2 {
		hub := &captureHub{target: id, received: make(chan receivedEvent, 1)}
		ps, err := NewPubSub(client, hub, logger)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { defer close(done); ps.Listen(ctx) }()
		t.Cleanup(func() { ps.Close(); <-done })
		hubs = append(hubs, hub)
	}
	if err := client.Publish(ctx, ChannelKey(id), []byte(`{"type":"message","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}`)); err != nil {
		t.Fatal(err)
	}
	for _, hub := range hubs {
		select {
		case ev := <-hub.received:
			var public map[string]any
			if json.Unmarshal(ev.payload, &public) != nil || public["type"] != "message" || public["traceparent"] != nil {
				t.Fatalf("unexpected public envelope: %s", ev.payload)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("first published event did not reach both gateways")
		}
	}
}
