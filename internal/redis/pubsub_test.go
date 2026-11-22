package redis

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
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

func TestBridgePreservesPublicFieldsAndStripsInternalTracing(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		unchanged         bool
	}{
		{"legacy public frame", ` {"type":"message","message":{"id":12},"extra":false} `, `{"type":"message","message":{"id":12},"extra":false}`, true},
		{"trace after event", `{"type":"message","traceparent":"old","extra":8}`, `{"type":"message","extra":8}`, false},
		{"escaped private key", `{"trace\u0070arent":"old","type":"message"}`, `{"type":"message"}`, false},
		{"private key first", `{"traceparent":null,"type":"message","message":{"traceparent":"public"}}`, `{"type":"message","message":{"traceparent":"public"}}`, false},
		{"nested arbitrary text", `{"type":"message","message":{"note":"traceparent","body":"\u03b1"}}`, `{"type":"message","message":{"note":"traceparent","body":"α"}}`, true},
		{"malformed object", `{"type":`, "", false},
		{"malformed traced object", `{"traceparent":`, "", false},
		{"nonobject", `[]`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &captureHub{target: uuid.New(), received: make(chan receivedEvent, 1)}
			ps := PubSub{hub: h}
			ps.deliver(context.Background(), h.target, []byte(tc.input))
			select {
			case got := <-h.received:
				if tc.want == "" {
					t.Fatalf("invalid frame reached broadcaster: %s", got.payload)
				}
				var actual, expected map[string]any
				if json.Unmarshal(got.payload, &actual) != nil || json.Unmarshal([]byte(tc.want), &expected) != nil || !reflect.DeepEqual(actual, expected) {
					t.Fatalf("got %s; want %s", got.payload, tc.want)
				}
				if tc.unchanged && string(got.payload) != tc.input {
					t.Fatalf("public frame changed: %s", got.payload)
				}
			default:
				if tc.want != "" {
					t.Fatal("valid frame did not reach broadcaster")
				}
			}
		})
	}
}
