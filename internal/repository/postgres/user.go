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

type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

func (s *UserStore) GetByID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*models.User, error) {
	query := `
		SELECT id, tenant_id, email, display_name, created_at
		FROM users
		WHERE id = $1 AND tenant_id = $2`

	var u models.User
	err := s.pool.QueryRow(ctx, query, userID, tenantID).Scan(
		&u.ID,
		&u.TenantID,
		&u.Email,
		&u.DisplayName,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}
