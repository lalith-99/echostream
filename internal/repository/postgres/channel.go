package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
)

type ChannelStore struct {
	pool *pgxpool.Pool
}

func NewChannelStore(pool *pgxpool.Pool) *ChannelStore {
	return &ChannelStore{pool: pool}
}

func (s *ChannelStore) Create(ctx context.Context, tenantID uuid.UUID, name string, isPrivate bool) (*models.Channel, error) {
	query := `
		INSERT INTO channels (id, tenant_id, name, is_private, created_at)
		VALUES (uuid_generate_v4(), $1, $2, $3, now())
		RETURNING id, tenant_id, name, is_private, created_at`

	var ch models.Channel
	err := s.pool.QueryRow(ctx, query, tenantID, name, isPrivate).Scan(
		&ch.ID,
		&ch.TenantID,
		&ch.Name,
		&ch.IsPrivate,
		&ch.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert channel: %w", err)
	}
	return &ch, nil
}

func (s *ChannelStore) GetByID(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID) (*models.Channel, error) {
	query := `
		SELECT id, tenant_id, name, is_private, created_at
		FROM channels
		WHERE id = $1 AND tenant_id = $2`

	var ch models.Channel
	err := s.pool.QueryRow(ctx, query, channelID, tenantID).Scan(
		&ch.ID,
		&ch.TenantID,
		&ch.Name,
		&ch.IsPrivate,
		&ch.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return &ch, nil
}

func (s *ChannelStore) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]models.Channel, error) {
	query := `
		SELECT id, tenant_id, name, is_private, created_at
		FROM channels
		WHERE tenant_id = $1
		ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	channels := make([]models.Channel, 0)
	for rows.Next() {
		var ch models.Channel
		if err := rows.Scan(
			&ch.ID,
			&ch.TenantID,
			&ch.Name,
			&ch.IsPrivate,
			&ch.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels: %w", err)
	}

	return channels, nil
}
