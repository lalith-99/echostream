package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
)

type MessageStore struct {
	pool *pgxpool.Pool
}

// NewMessageStore returns a Postgres-backed message store.
func NewMessageStore(pool *pgxpool.Pool) *MessageStore {
	return &MessageStore{pool: pool}
}

func (s *MessageStore) Create(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID, senderID uuid.UUID, body string) (*models.Message, error) {
	query := `
		INSERT INTO messages (channel_id, sender_id, body, created_at)
		VALUES ($1, $2, $3, now())
		RETURNING id, channel_id, sender_id, body, created_at`

	var msg models.Message
	err := s.pool.QueryRow(ctx, query, channelID, senderID, body).Scan(
		&msg.ID,
		&msg.ChannelID,
		&msg.SenderID,
		&msg.Body,
		&msg.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	return &msg, nil
}

func (s *MessageStore) ListByChannel(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID, before int64, limit int) ([]models.Message, error) {
	// Cursor-based pagination: before=0 means latest, before=N means older than ID N.
	var query string
	var args []any

	if before > 0 {
		query = `
			SELECT id, channel_id, sender_id, body, created_at
			FROM messages
			WHERE channel_id = $1 AND id < $2
			ORDER BY id DESC
			LIMIT $3`
		args = []any{channelID, before, limit}
	} else {
		query = `
			SELECT id, channel_id, sender_id, body, created_at
			FROM messages
			WHERE channel_id = $1
			ORDER BY id DESC
			LIMIT $2`
		args = []any{channelID, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]models.Message, 0)
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(
			&msg.ID,
			&msg.ChannelID,
			&msg.SenderID,
			&msg.Body,
			&msg.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return messages, nil
}
