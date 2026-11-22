package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/repository"
)

type ChannelStore struct {
	pool *pgxpool.Pool
}

// NewChannelStore returns a ChannelStore backed by Postgres.
func NewChannelStore(pool *pgxpool.Pool) *ChannelStore {
	return &ChannelStore{pool: pool}
}

func (s *ChannelStore) Create(ctx context.Context, tenantID, creatorID uuid.UUID, name string, isPrivate bool) (*models.Channel, error) {
	query := `
		WITH created AS (
			INSERT INTO channels (tenant_id, name, is_private)
			SELECT tenant_id, $3, $4 FROM users WHERE id = $2 AND tenant_id = $1
			RETURNING id, tenant_id, name, is_private, created_at
		), owner AS (
			INSERT INTO channel_members (channel_id, user_id, role)
			SELECT id, $2, 'admin' FROM created
			RETURNING channel_id
		)
		SELECT id, tenant_id, name, is_private, created_at
		FROM created JOIN owner ON owner.channel_id = created.id`

	var ch models.Channel
	err := s.pool.QueryRow(ctx, query, tenantID, creatorID, name, isPrivate).Scan(
		&ch.ID,
		&ch.TenantID,
		&ch.Name,
		&ch.IsPrivate,
		&ch.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrMembershipUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("insert channel: %w", err)
	}
	return &ch, nil
}

// visibleChannels also validates the viewer's current workspace. It deliberately
// ignores historical membership rows that connect users to a different workspace.
const visibleChannels = `
	SELECT c.id, c.tenant_id, c.name, c.is_private, c.created_at
	FROM channels c JOIN users viewer ON viewer.tenant_id = c.tenant_id
	WHERE c.tenant_id = $1 AND viewer.id = $2
	  AND (NOT c.is_private OR EXISTS (
		  SELECT 1 FROM channel_members m WHERE m.channel_id = c.id AND m.user_id = viewer.id
	  ))`

func (s *ChannelStore) GetByID(ctx context.Context, tenantID, channelID, viewerID uuid.UUID) (*models.Channel, error) {
	const query = visibleChannels + ` AND c.id = $3`

	var ch models.Channel
	err := s.pool.QueryRow(ctx, query, tenantID, viewerID, channelID).Scan(
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

func (s *ChannelStore) ListByTenant(ctx context.Context, tenantID, userID uuid.UUID, limit, offset int) ([]models.Channel, error) {
	const query = visibleChannels + ` ORDER BY c.created_at DESC, c.id LIMIT $3 OFFSET $4`

	rows, err := s.pool.Query(ctx, query, tenantID, userID, limit, offset)
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
