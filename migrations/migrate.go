package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.up.sql
var files embed.FS

type version struct {
	Number   int
	Filename string
	Checksum string
}

type migration struct {
	version
	sql string
}

var manifest = sync.OnceValues(func() ([]migration, error) {
	names, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no database migrations in this build")
	}
	var entries []migration
	for i, name := range names {
		prefix, _, _ := strings.Cut(name, "_")
		number, err := strconv.Atoi(prefix)
		if err != nil || number != i+1 {
			return nil, fmt.Errorf("expected migration %03d, found %s", i+1, name)
		}
		sql, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		entries = append(entries, migration{
			version: version{number, name, fmt.Sprintf("%x", sha256.Sum256(sql))},
			sql:     string(sql),
		})
	}
	return entries, nil
})

// Ready checks that this binary's latest migration was committed.
// Older app instances can keep serving during a compatible rolling upgrade.
func Ready(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := manifest()
	if err != nil {
		return err
	}
	latest := entries[len(entries)-1]
	var ok bool
	err = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM echostream_schema_migrations
		WHERE version=$1 AND filename=$2 AND checksum=$3)`, latest.Number, latest.Filename, latest.Checksum).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("database requires migration %s", latest.Filename)
	}
	return nil
}

// Apply serializes deployers and records the exact SQL that ran. Both the
// schema changes and their ledger entries roll back if any step fails.
func Apply(ctx context.Context, conn *pgx.Conn) error {
	entries, err := manifest()
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Stable application-specific lock, held until commit or rollback.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1829944305, 1)"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS echostream_schema_migrations (
			version integer PRIMARY KEY, filename text NOT NULL, checksum text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}

		rows, err := tx.Query(ctx, "SELECT version, filename, checksum FROM echostream_schema_migrations ORDER BY version")
		if err != nil {
			return err
		}
		applied, err := pgx.CollectRows(rows, pgx.RowToStructByPos[version])
		if err != nil {
			return err
		}
		if len(applied) > len(entries) {
			return fmt.Errorf("database has newer migrations than this build")
		}
		for i, m := range entries {
			if i < len(applied) {
				if applied[i] != m.version {
					return fmt.Errorf("migration history differs at version %d (%s)", m.Number, m.Filename)
				}
				continue
			}
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return fmt.Errorf("apply %s: %w", m.Filename, err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO echostream_schema_migrations(version, filename, checksum) VALUES ($1,$2,$3)", m.Number, m.Filename, m.Checksum); err != nil {
				return err
			}
		}
		return nil
	})
}
