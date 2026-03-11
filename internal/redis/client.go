package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Client wraps the Redis client used by the app.
type Client struct {
	rdb    *goredis.Client
	logger *zap.Logger
}

// NewClient connects to Redis and verifies the connection.
func NewClient(redisURL string, logger *zap.Logger) (*Client, error) {
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	rdb := goredis.NewClient(opts)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	logger.Info("Redis connection established", zap.String("addr", opts.Addr))
	return &Client{rdb: rdb, logger: logger}, nil
}

// Close shuts down the Redis client.
func (c *Client) Close() {
	c.logger.Info("closing Redis connection")
	if err := c.rdb.Close(); err != nil {
		c.logger.Error("failed to close Redis", zap.Error(err))
	}
}

// Publish sends a message to a Redis pub/sub channel.
func (c *Client) Publish(ctx context.Context, channel string, payload []byte) error {
	return c.rdb.Publish(ctx, channel, payload).Err()
}

// RDB returns the underlying go-redis client.
func (c *Client) RDB() *goredis.Client {
	return c.rdb
}
