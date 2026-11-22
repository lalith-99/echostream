package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/observ"
	"github.com/lalith-99/echostream/internal/repository"
)

type MessageStore struct {
	pool   *pgxpool.Pool
	wakeup chan struct{}
}

func NewMessageStore(pool *pgxpool.Pool) *MessageStore {
	return &MessageStore{pool: pool, wakeup: make(chan struct{}, 1)}
}

// Include both tenant checks to handle memberships imported from older releases.
const messageMembership = `
	SELECT 1 FROM channels c
	JOIN channel_members cm ON cm.channel_id = c.id
	JOIN users u ON u.id = cm.user_id AND u.tenant_id = c.tenant_id
	WHERE c.tenant_id = $1 AND c.id = $2 AND cm.user_id = $3`

const messageFields = `id, channel_id, sender_id, body, created_at`

func scanMessage(row pgx.Row) (*models.Message, error) {
	var m models.Message
	err := row.Scan(&m.ID, &m.ChannelID, &m.SenderID, &m.Body, &m.CreatedAt)
	return &m, err
}

func checkMessageMembership(ctx context.Context, tx pgx.Tx, query string, tenantID, channelID, userID uuid.UUID) error {
	var authorized int
	err := tx.QueryRow(ctx, query, tenantID, channelID, userID).Scan(&authorized)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrMessageForbidden
	}
	return err
}

func (s *MessageStore) Create(ctx context.Context, tenantID, channelID, senderID uuid.UUID, body string, requestKey *uuid.UUID) (*models.Message, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	// Serialize sends to the same channel BEFORE allocating the bigserial ID.
	// A committed high-water mark can then never skip a later commit with a
	// smaller ID. Other channels can write independently. Locking membership
	// prevents a concurrent leave between authorization and insertion.
	err = checkMessageMembership(ctx, tx, messageMembership+" FOR NO KEY UPDATE OF c FOR SHARE OF cm", tenantID, channelID, senderID)
	if err != nil {
		return nil, false, err
	}

	if requestKey != nil {
		existing, err := scanMessage(tx.QueryRow(ctx, "SELECT "+messageFields+` FROM messages
			WHERE tenant_id = $1 AND channel_id = $2 AND sender_id = $3 AND request_key = $4`,
			tenantID, channelID, senderID, requestKey))
		switch {
		case err == nil && existing.Body != body:
			return nil, false, repository.ErrMessageKeyConflict
		case err == nil:
			return existing, false, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, false, fmt.Errorf("look up message retry: %w", err)
		}
	}

	msg, err := scanMessage(tx.QueryRow(ctx, `INSERT INTO messages (tenant_id, channel_id, sender_id, body, request_key)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+messageFields, tenantID, channelID, senderID, body, requestKey))
	if err != nil {
		return nil, false, fmt.Errorf("insert message: %w", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO message_outbox (message_id, traceparent) VALUES ($1, $2)", msg.ID, observ.Traceparent(ctx)); err != nil {
		return nil, false, fmt.Errorf("enqueue message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	// Coalesce hints; another process may already have claimed this row.
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
	return msg, true, nil
}

func (s *MessageStore) ListByChannel(ctx context.Context, tenantID, channelID, userID uuid.UUID, page repository.MessagePage) ([]models.Message, error) {
	// Check membership and read history at one consistent point in time.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := checkMessageMembership(ctx, tx, messageMembership, tenantID, channelID, userID); err != nil {
		return nil, err
	}

	query := "SELECT " + messageFields + ` FROM messages WHERE tenant_id = $1 AND channel_id = $2`
	args := []any{tenantID, channelID, page.Limit}
	switch {
	case page.After != nil:
		query += " AND id > $4 ORDER BY id ASC"
		args = append(args, *page.After)
	case page.Before > 0:
		query += " AND id < $4 ORDER BY id DESC"
		args = append(args, page.Before)
	default:
		query += " ORDER BY id DESC"
	}

	rows, err := tx.Query(ctx, query+" LIMIT $3", args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]models.Message, 0)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, tx.Commit(ctx)
}
