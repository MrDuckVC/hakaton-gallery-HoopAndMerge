// Package postgres implements PostgreSQL infrastructure and migration boundaries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryTimeout bounds individual database operations, in addition to caller deadlines.
const QueryTimeout = 5 * time.Second

// Open connects and pings PostgreSQL. No schema changes are performed here.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("DATABASE_URL: invalid PostgreSQL connection configuration")
	}
	cfg.MaxConns = 10
	cfg.ConnConfig.ConnectTimeout = QueryTimeout
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, safeError(ctx, "initialize PostgreSQL connection pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, safeError(ctx, "connect to PostgreSQL; check DATABASE_URL and database availability")
	}
	return pool, nil
}

// Querier is the adapter-local query capability shared by a pool and acquired connection.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// CheckSchema reads migration history without initializing goose or modifying the database.
func CheckSchema(ctx context.Context, db Querier) error {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	rows, err := db.Query(ctx, `SELECT version_id FROM (
  SELECT DISTINCT ON (version_id) version_id, is_applied
  FROM goose_db_version ORDER BY version_id, id DESC
 ) AS latest WHERE is_applied ORDER BY version_id`)
	if err != nil {
		return safeError(ctx, "schema unavailable; apply migrations with make migrate-up and check database permissions")
	}
	defer rows.Close()
	versions := make([]int64, 0, 4)
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return errors.New("invalid migration history; check database schema")
		}
		versions = append(versions, version)
	}
	if rows.Err() != nil {
		return safeError(ctx, "read migration history")
	}
	return compatibleVersions(versions)
}

func compatibleVersions(applied []int64) error {
	sources, err := fs.Glob(migrations.Files(), "*.sql")
	if err != nil {
		return fmt.Errorf("read embedded migration names: %w", err)
	}
	expected := make([]int64, 1, len(sources)+1)
	for _, source := range sources {
		prefix, _, _ := strings.Cut(source, "_")
		version, parseErr := strconv.ParseInt(prefix, 10, 64)
		if parseErr != nil {
			return errors.New("invalid embedded migration version")
		}
		expected = append(expected, version)
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i] < expected[j] })
	if len(applied) != len(expected) {
		return errors.New("incompatible database schema: apply make migrate-up; a newer schema requires its matching backend binary")
	}
	for i, version := range expected {
		if applied[i] != version {
			return errors.New("incompatible database migration set: run make migrate-up or use the matching backend binary")
		}
	}
	return nil
}

// safeError deliberately excludes driver errors, which can contain DSNs, SQL values and credentials.
func safeError(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s failed", operation)
}
