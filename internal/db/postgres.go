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

// New creates a connection pool from a Postgres URL.
func New(ctx context.Context, databaseURL string, logger *zap.Logger) (*DB, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	// Pool tuning
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 20 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Verify the connection works before returning.
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
