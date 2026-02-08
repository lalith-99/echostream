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

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

func New(ctx context.Context, cfg Config, logger *zap.Logger) (*DB, error) {
	var dsn string
	if cfg.Password != "" {
		dsn = fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database,
		)
	} else {
		dsn = fmt.Sprintf(
			"host=%s port=%d user=%s dbname=%s",
			cfg.Host, cfg.Port, cfg.User, cfg.Database,
		)
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("sparse pool config: %w", err)
	}
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 20 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping DB: %w", err)
	}

	logger.Info("DB connection established",
		zap.String("host", cfg.Host),
		zap.Int("port", cfg.Port),
		zap.String("database", cfg.Database),
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
