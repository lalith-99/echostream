package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/observ"
	"github.com/lalith-99/echostream/internal/websocket"
	"go.uber.org/zap"
)

type EventPublisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

type outboxStore interface {
	Wakeups() <-chan struct{}
	DispatchNext(context.Context, func(context.Context, *models.Message) error) (bool, error)
}

// RunOutbox drains committed sends until cancellation. Multiple servers can
// run this worker. A websocket client deduplicates by durable message ID.
func RunOutbox(ctx context.Context, store outboxStore, publisher EventPublisher, logger *zap.Logger) {
	publish := func(ctx context.Context, msg *models.Message) error {
		channel := msg.ChannelID.String()
		event := struct {
			websocket.OutboundEvent
			Traceparent string `json:"traceparent,omitempty"`
		}{
			OutboundEvent: websocket.OutboundEvent{Type: "message", ChannelID: channel, Message: msg},
			Traceparent:   observ.Traceparent(ctx),
		}
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		publishCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return publisher.Publish(publishCtx, "ch:"+channel, data)
	}

	for ctx.Err() == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		found, err := store.DispatchNext(attemptCtx, publish)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil && found {
			continue
		}
		wake := store.Wakeups()
		delay := 250 * time.Millisecond
		if err != nil {
			logger.Warn("outbox dispatch failed", zap.Error(err))
			wake = nil // New sends must not defeat outage backoff.
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(delay):
		}
	}
}
