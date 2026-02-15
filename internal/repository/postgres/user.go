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

// Create inserts a new user row. Postgres generates the UUID and timestamp.
func (s *UserStore) Create(ctx context.Context, tenantID uuid.UUID, email, displayName, passwordHash string) (*models.User, error) {
	query := `
		INSERT INTO users (tenant_id, email, display_name, password_hash, created_at)
		VALUES ($1, $2, $3, $4, now())
		RETURNING id, tenant_id, email, display_name, password_hash, created_at`

	var u models.User
	err := s.pool.QueryRow(ctx, query, tenantID, email, displayName, passwordHash).Scan(
		&u.ID,
		&u.TenantID,
		&u.Email,
		&u.DisplayName,
		&u.PasswordHash,
		&u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return &u, nil
}

func (s *UserStore) GetByID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*models.User, error) {
	query := `
		SELECT id, tenant_id, email, display_name, password_hash, created_at
		FROM users
		WHERE id = $1 AND tenant_id = $2`

	var u models.User
	err := s.pool.QueryRow(ctx, query, userID, tenantID).Scan(
		&u.ID,
		&u.TenantID,
		&u.Email,
		&u.DisplayName,
		&u.PasswordHash,
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

// GetByEmail looks up a user by email (globally, not tenant-scoped).
// Used for login — you type your email, we find you.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, tenant_id, email, display_name, password_hash, created_at
		FROM users
		WHERE email = $1`

	var u models.User
	err := s.pool.QueryRow(ctx, query, email).Scan(
		&u.ID,
		&u.TenantID,
		&u.Email,
		&u.DisplayName,
		&u.PasswordHash,
		&u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &u, nil
}
