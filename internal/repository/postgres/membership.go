package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/repository"
)

type MembershipStore struct {
	pool *pgxpool.Pool
}

// NewMembershipStore creates a membership store backed by Postgres.
func NewMembershipStore(pool *pgxpool.Pool) *MembershipStore {
	return &MembershipStore{pool: pool}
}

func (s *MembershipStore) AddMember(ctx context.Context, tenantID, channelID, actorID, targetID uuid.UUID) error {
	// Check both users in the channel workspace at the point of insertion.
	// Public self-joins and invites from existing members use the same rule.
	const query = `
		WITH eligible AS (
			SELECT c.id AS channel_id, target.id AS user_id
			FROM channels c
			JOIN users actor ON actor.id = $3 AND actor.tenant_id = c.tenant_id
			JOIN users target ON target.id = $4 AND target.tenant_id = c.tenant_id
			WHERE c.tenant_id = $1 AND c.id = $2
			  AND ((actor.id = target.id AND NOT c.is_private) OR EXISTS (
			    SELECT 1 FROM channel_members m WHERE m.channel_id = c.id AND m.user_id = actor.id
			    FOR KEY SHARE
			  ))
		), added AS (
			INSERT INTO channel_members (channel_id, user_id, role)
			SELECT channel_id, user_id, 'member' FROM eligible
			ON CONFLICT (channel_id, user_id) DO NOTHING
		)
		SELECT EXISTS (SELECT 1 FROM eligible)`
	var allowed bool
	if err := s.pool.QueryRow(ctx, query, tenantID, channelID, actorID, targetID).Scan(&allowed); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if !allowed {
		return repository.ErrMembershipUnavailable
	}
	return nil
}

func (s *MembershipStore) RemoveMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) error {
	query := `
		DELETE FROM channel_members
		WHERE channel_id = $1 AND user_id = $2`

	_, err := s.pool.Exec(ctx, query, channelID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

func (s *MembershipStore) ListMembers(ctx context.Context, tenantID, channelID, viewerID uuid.UUID, limit, offset int) ([]models.ChannelMember, error) {
	const query = `WITH visible AS (` + visibleChannels + ` AND c.id = $3)
		SELECT m.channel_id, m.user_id, m.role
		FROM visible v
		JOIN channel_members m ON m.channel_id = v.id
		JOIN users u ON u.id = m.user_id AND u.tenant_id = v.tenant_id
		ORDER BY m.user_id LIMIT $4 OFFSET $5`

	rows, err := s.pool.Query(ctx, query, tenantID, viewerID, channelID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	members := make([]models.ChannelMember, 0)
	for rows.Next() {
		var m models.ChannelMember
		if err := rows.Scan(&m.ChannelID, &m.UserID, &m.Role); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}

	return members, nil
}

func (s *MembershipStore) IsMember(ctx context.Context, tenantID, channelID, userID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM channel_members m
			JOIN channels c ON c.id = m.channel_id
			JOIN users u ON u.id = m.user_id AND u.tenant_id = c.tenant_id
			WHERE c.tenant_id = $1 AND m.channel_id = $2 AND m.user_id = $3
		)`

	var exists bool
	err := s.pool.QueryRow(ctx, query, tenantID, channelID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return exists, nil
}
