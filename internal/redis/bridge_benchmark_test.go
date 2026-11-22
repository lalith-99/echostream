package redis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// No network or socket writes: measure only the gateway's Redis envelope
// processing and handoff to its broadcaster.
type benchmarkHub struct{ bytes int }

func (h *benchmarkHub) Broadcast(_ uuid.UUID, data []byte) { h.bytes += len(data) }
func (h *benchmarkHub) Resync()                            {}

func BenchmarkBridgeDelivery(b *testing.B) {
	id := uuid.MustParse("7f01b504-d532-4f35-a688-73fd45da1340")
	for _, test := range []struct{ name, traceparent string }{
		{"untraced", ""}, {"traced", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	} {
		b.Run(test.name, func(b *testing.B) {
			public := map[string]any{
				"type": "message", "channel_id": id,
				"message": map[string]any{"id": 42, "channel_id": id, "sender_id": "7bd5cc7b-2c18-4e14-a2f8-da3ebf8dd1b1", "body": strings.Repeat("team update: deploying at 16:00 UTC. ", 12), "created_at": "2026-03-01T16:00:00Z"},
			}
			if test.traceparent != "" {
				public["traceparent"] = test.traceparent
			}
			raw, err := json.Marshal(public)
			if err != nil {
				b.Fatal(err)
			}
			h := &benchmarkHub{}
			ps := &PubSub{hub: h}
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ps.deliver(context.Background(), id, raw)
			}
			if h.bytes == 0 {
				b.Fatal("message was not delivered")
			}
		})
	}
}
