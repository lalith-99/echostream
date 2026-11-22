package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/observ"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Broadcaster interface {
	Broadcast(channelID uuid.UUID, data []byte)
	Resync()
}

// Each instance has one subscription, established before the HTTP server
// starts. The hub routes each event exclusively to local channel subscribers.
type PubSub struct {
	sub *goredis.PubSub
	hub Broadcaster
}

func NewPubSub(client *Client, hub Broadcaster, logger *zap.Logger) (*PubSub, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := client.rdb.PSubscribe(ctx, "ch:*")
	reply, err := sub.ReceiveTimeout(ctx, 5*time.Second)
	if err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("wait for Redis subscription: %w", err)
	}
	ack, ok := reply.(*goredis.Subscription)
	if !ok || ack.Kind != "psubscribe" || ack.Channel != "ch:*" {
		_ = sub.Close()
		return nil, fmt.Errorf("unexpected Redis subscription acknowledgement")
	}
	logger.Info("Redis message subscription established")
	return &PubSub{sub: sub, hub: hub}, nil
}

func ChannelKey(channelID uuid.UUID) string {
	return "ch:" + channelID.String()
}

func (ps *PubSub) Listen(ctx context.Context) {
	events := ps.sub.ChannelWithSubscriptions()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch e := event.(type) {
			case *goredis.Subscription:
				if e.Kind == "psubscribe" {
					ps.hub.Resync()
				}
			case *goredis.Message:
				key, found := strings.CutPrefix(e.Channel, "ch:")
				id, err := uuid.Parse(key)
				if found && err == nil {
					ps.deliver(ctx, id, []byte(e.Payload))
				}
			}
		}
	}
}

// Trace propagation is internal to the relay and gateway. Only the public
// event fields are forwarded to the browser.
func (ps *PubSub) deliver(ctx context.Context, channel uuid.UUID, payload []byte) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope == nil {
		return
	}
	var parent string
	if value, ok := envelope["traceparent"]; ok {
		_ = json.Unmarshal(value, &parent)
		delete(envelope, "traceparent")
		payload, _ = json.Marshal(envelope)
	}
	_, span := observ.StartLinked(ctx, parent, "redis.fanout")
	defer span.End()
	ps.hub.Broadcast(channel, payload)
}

func (ps *PubSub) Close() { _ = ps.sub.Close() }
