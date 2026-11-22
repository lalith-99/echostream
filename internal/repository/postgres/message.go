package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	FROM channels c
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
	err = checkMessageMembership(ctx, tx, "SELECT 1 "+messageMembership+" FOR NO KEY UPDATE OF c FOR SHARE OF cm", tenantID, channelID, senderID)
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
	// A single statement gives the authorization check and the keyset page the
	// same MVCC snapshot. No membership produces zero rows. An authorized empty
	// page produces one outer-join row with a NULL message ID.
	order, cursor := "DESC", ""
	args := []any{tenantID, channelID, userID, page.Limit}
	switch {
	case page.After != nil:
		order, cursor = "ASC", " AND id > $5"
		args = append(args, *page.After)
	case page.Before > 0:
		cursor = " AND id < $5"
		args = append(args, page.Before)
	}

	query := `SELECT p.id, p.sender_id, p.body, p.created_at
		FROM (SELECT c.id ` + messageMembership + `) AS member
		LEFT JOIN LATERAL (SELECT id, sender_id, body, created_at FROM messages
			WHERE tenant_id = $1 AND channel_id = member.id` + cursor +
		" ORDER BY id " + order + " LIMIT $4) AS p ON true ORDER BY p.id " + order
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]models.Message, 0, min(max(page.Limit, 0), 100))
	authorized := false
	for rows.Next() {
		authorized = true
		if rows.RawValues()[0] == nil {
			continue
		}
		messages = append(messages, models.Message{ChannelID: channelID})
		msg := &messages[len(messages)-1]
		// Decode the binary UUID directly, without formatting and parsing text.
		var sender pgtype.UUID
		if err := rows.Scan(&msg.ID, &sender, &msg.Body, &msg.CreatedAt); err != nil {
			return nil, err
		}
		msg.SenderID = uuid.UUID(sender.Bytes)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !authorized {
		return nil, repository.ErrMessageForbidden
	}
	return messages, nil
}
