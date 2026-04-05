package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
)

// SignupStore implements repository.SignupRepository.
// It wraps the tenant + user creation in a Postgres transaction.
type SignupStore struct {
	pool *pgxpool.Pool
}

// NewSignupStore initializes a SignupStore.
func NewSignupStore(pool *pgxpool.Pool) *SignupStore {
	return &SignupStore{pool: pool}
}

// CreateTenantAndUser inserts a tenant and its first user atomically.
//
// Transaction guarantees:
//   - If the user INSERT fails, the tenant INSERT is rolled back.
//   - defer tx.Rollback() is a no-op after a successful Commit().
//   - The caller never sees Postgres internals — just models and errors.
func (s *SignupStore) CreateTenantAndUser(
	ctx context.Context,
	tenantName, email, displayName, passwordHash string,
) (*models.Tenant, *models.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin signup tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	var tenant models.Tenant
	err = tx.QueryRow(ctx,
		`INSERT INTO tenants (name, created_at)
		 VALUES ($1, now())
		 RETURNING id, name, created_at`,
		tenantName,
	).Scan(&tenant.ID, &tenant.Name, &tenant.CreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("insert tenant: %w", err)
	}

	var user models.User
	err = tx.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, display_name, password_hash, created_at)
		 VALUES ($1, $2, $3, $4, now())
		 RETURNING id, tenant_id, email, display_name, password_hash, created_at`,
		tenant.ID, email, displayName, passwordHash,
	).Scan(&user.ID, &user.TenantID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("insert user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit signup tx: %w", err)
	}
	return &tenant, &user, nil
}
