package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
)

type MembershipStore struct {
	pool *pgxpool.Pool
}

// NewMembershipStore creates a membership store backed by Postgres.
func NewMembershipStore(pool *pgxpool.Pool) *MembershipStore {
	return &MembershipStore{pool: pool}
}

func (s *MembershipStore) AddMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID, role string) error {
	// ON CONFLICT DO NOTHING makes this idempotent.
	query := `
		INSERT INTO channel_members (channel_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id, user_id) DO NOTHING`

	_, err := s.pool.Exec(ctx, query, channelID, userID, role)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
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

func (s *MembershipStore) ListMembers(ctx context.Context, channelID uuid.UUID) ([]models.ChannelMember, error) {
	query := `
		SELECT channel_id, user_id, role
		FROM channel_members
		WHERE channel_id = $1
		ORDER BY user_id`

	rows, err := s.pool.Query(ctx, query, channelID)
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

func (s *MembershipStore) IsMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM channel_members
			WHERE channel_id = $1 AND user_id = $2
		)`

	var exists bool
	err := s.pool.QueryRow(ctx, query, channelID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return exists, nil
}
