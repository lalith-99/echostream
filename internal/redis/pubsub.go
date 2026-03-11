package redis

import (
	"context"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Broadcaster can send data to all local clients in a channel.
type Broadcaster interface {
	Broadcast(channelID uuid.UUID, data []byte)
}

// PubSub bridges Redis pub/sub with the local WebSocket hub.
type PubSub struct {
	client *Client
	sub    *goredis.PubSub
	hub    Broadcaster
	logger *zap.Logger
}

// NewPubSub creates a Redis pub/sub bridge for the local hub.
func NewPubSub(client *Client, hub Broadcaster, logger *zap.Logger) *PubSub {
	return &PubSub{
		// Start with no channels; callers subscribe as channels become active.
		sub:    client.rdb.Subscribe(context.Background()),
		client: client,
		hub:    hub,
		logger: logger,
	}
}

// ChannelKey returns the Redis pub/sub key for a channel UUID.
func ChannelKey(channelID uuid.UUID) string {
	return "ch:" + channelID.String()
}

// Subscribe tells Redis to start delivering messages for this channel.
func (ps *PubSub) Subscribe(channelID uuid.UUID) {
	key := ChannelKey(channelID)
	if err := ps.sub.Subscribe(context.Background(), key); err != nil {
		ps.logger.Error("redis subscribe failed", zap.String("channel", key), zap.Error(err))
	}
}

// Unsubscribe tells Redis to stop delivering messages for this channel.
func (ps *PubSub) Unsubscribe(channelID uuid.UUID) {
	key := ChannelKey(channelID)
	if err := ps.sub.Unsubscribe(context.Background(), key); err != nil {
		ps.logger.Error("redis unsubscribe failed", zap.String("channel", key), zap.Error(err))
	}
}

// Listen reads messages from Redis and forwards them to the Hub.
func (ps *PubSub) Listen(ctx context.Context) {
	ch := ps.sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Channel keys are stored as "ch:<uuid>".
			if len(msg.Channel) <= 3 {
				continue
			}
			channelID, err := uuid.Parse(msg.Channel[3:])
			if err != nil {
				ps.logger.Warn("invalid channel key from redis", zap.String("key", msg.Channel))
				continue
			}
			ps.hub.Broadcast(channelID, []byte(msg.Payload))
		}
	}
}

// Close releases the Redis pub/sub subscription.
func (ps *PubSub) Close() {
	if err := ps.sub.Close(); err != nil {
		ps.logger.Error("failed to close Redis pubsub", zap.Error(err))
	}
}
