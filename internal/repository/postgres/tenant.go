package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/models"
)

type TenantStore struct {
	pool *pgxpool.Pool
}

func NewTenantStore(pool *pgxpool.Pool) *TenantStore {
	return &TenantStore{pool: pool}
}

func (s *TenantStore) Create(ctx context.Context, name string) (*models.Tenant, error) {
	query := `
		INSERT INTO tenants (name, created_at)
		VALUES ($1, now())
		RETURNING id, name, created_at`

	var t models.Tenant
	err := s.pool.QueryRow(ctx, query, name).Scan(
		&t.ID,
		&t.Name,
		&t.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return &t, nil
}
