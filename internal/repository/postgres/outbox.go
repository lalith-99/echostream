package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/observ"
)

// Wakeups signals committed local sends. Polling covers other processes and restarts.
func (s *MessageStore) Wakeups() <-chan struct{} { return s.wakeup }

// DispatchNext locks one due message across workers, releasing the lock when
// its publish attempt completes. The caller puts a short deadline on publish.
// A crash after publish and before commit leaves the row available for redelivery.
func (s *MessageStore) DispatchNext(ctx context.Context, publish func(context.Context, *models.Message) error) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var msg models.Message
	var traceparent string
	err = tx.QueryRow(ctx, `SELECT m.id, m.channel_id, m.sender_id, m.body, m.created_at, o.traceparent
		FROM message_outbox o JOIN messages m ON m.id = o.message_id
		WHERE o.available_at <= now() ORDER BY o.available_at, o.message_id
		LIMIT 1 FOR UPDATE OF o SKIP LOCKED`).Scan(&msg.ID, &msg.ChannelID, &msg.SenderID, &msg.Body, &msg.CreatedAt, &traceparent)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	publishCtx, span := observ.StartLinked(ctx, traceparent, "outbox.publish")
	defer span.End()
	publishErr := publish(publishCtx, &msg)
	if publishErr != nil {
		span.RecordError(publishErr)
		// Retry indefinitely, starting at one second, capped at one minute.
		_, err = tx.Exec(ctx, `UPDATE message_outbox SET
			available_at = now() + LEAST(60, 1 << LEAST(attempts, 6)) * interval '1 second',
			attempts = LEAST(attempts::bigint + 1, 2147483647)::integer WHERE message_id = $1`, msg.ID)
	} else {
		_, err = tx.Exec(ctx, "DELETE FROM message_outbox WHERE message_id = $1", msg.ID)
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	if publishErr != nil {
		return true, fmt.Errorf("retry publish of message %d: %w", msg.ID, publishErr)
	}
	return true, nil
}
