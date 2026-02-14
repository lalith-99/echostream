package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type DB struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// New creates a database connection pool from a Postgres connection URL.
//
// Why take a URL string instead of individual host/port/user fields?
//   - pgxpool.ParseConfig() natively understands Postgres URLs
//     ("postgres://user:pass@host:5432/db?sslmode=disable").
//   - The URL is what config.Config already stores (DATABASE_URL env var).
//   - No manual DSN building = no chance of forgetting sslmode, escaping
//     special characters in passwords, etc.
//   - Standard in the industry: DATABASE_URL is the universal convention
//     (Heroku, Railway, RDS, every PaaS uses it).
func New(ctx context.Context, databaseURL string, logger *zap.Logger) (*DB, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	// Connection pool tuning — these are sensible defaults for a
	// messaging backend:
	//
	// MaxConns (25): upper bound on open connections. Each active
	//   WebSocket handler or API request may hold a connection briefly.
	//   25 handles high concurrency without overwhelming Postgres
	//   (RDS default max_connections is 100).
	//
	// MinConns (5): keep 5 warm connections ready. Avoids cold-start
	//   latency on the first few requests after idle periods.
	//
	// MaxConnLifetime (1h): recycle connections hourly. Prevents issues
	//   with stale TCP connections, DNS changes, or RDS failovers.
	//
	// MaxConnIdleTime (20min): close idle connections after 20 min.
	//   Frees up Postgres slots when traffic is low.
	//
	// HealthCheckPeriod (1min): ping idle connections every minute.
	//   Detects dead connections before a real query hits them.
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 20 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Ping verifies the connection actually works (credentials, network, etc.)
	// If it fails, we close the pool immediately — don't leak a half-open pool.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping DB: %w", err)
	}

	logger.Info("DB connection established",
		zap.String("dsn", poolConfig.ConnString()),
		zap.Int32("max_conns", poolConfig.MaxConns),
	)
	return &DB{
		pool:   pool,
		logger: logger,
	}, nil
}

func (db *DB) Close() {
	db.logger.Info("closing database connection pool")
	db.pool.Close()
}

func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

func (db *DB) Health(ctx context.Context) error {
	return db.pool.Ping(ctx)
}
