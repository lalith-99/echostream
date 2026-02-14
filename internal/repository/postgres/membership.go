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

func NewMembershipStore(pool *pgxpool.Pool) *MembershipStore {
	return &MembershipStore{pool: pool}
}

func (s *MembershipStore) AddMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID, role string) error {
	// ON CONFLICT DO NOTHING: if the user is already a member, this is a
	// no-op instead of an error. Why?
	//   - The API call "join channel" should be idempotent. Calling it twice
	//     shouldn't fail — it should just succeed silently the second time.
	//   - Without this, a duplicate (channel_id, user_id) would violate the
	//     primary key constraint and return a Postgres error.
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
	// DELETE is naturally idempotent: if the row doesn't exist, it deletes
	// zero rows — no error. So "leave channel" called twice is fine.
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
		WHERE channel_id = $1`

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
	// SELECT EXISTS wraps a subquery and returns a single boolean.
	// Postgres stops scanning as soon as it finds one matching row.
	//
	// Why not "SELECT COUNT(*) ... WHERE ... " and check count > 0?
	//   - COUNT scans ALL matching rows to count them. Wasteful.
	//   - EXISTS stops at the first match. O(1) vs O(n).
	//   - For a hot-path check (every message send), this matters.
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
