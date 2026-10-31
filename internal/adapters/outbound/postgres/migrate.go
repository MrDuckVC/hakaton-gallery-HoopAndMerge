package postgres

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// MigrationStatus is the administrative output, independent of goose's implementation types.
type MigrationStatus struct {
	Version int64
	Applied bool
}

// Migrate supports forward-only administration. Server startup never calls it.
func Migrate(ctx context.Context, dsn, operation string) ([]MigrationStatus, error) {
	if operation != "up" && operation != "status" {
		return nil, errors.New("migration operation must be up or status")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("DATABASE_URL: invalid PostgreSQL connection configuration")
	}
	cfg.ConnectTimeout = QueryTimeout
	cfg.RuntimeParams["statement_timeout"] = "60000"
	cfg.RuntimeParams["lock_timeout"] = "5000"
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	db := stdlib.OpenDB(*cfg)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		return nil, safeError(ctx, "connect for migrations; check DATABASE_URL and database availability")
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 55), lock.WithUnlockTimeout(1, 1))
	if err != nil {
		return nil, errors.New("initialize migration lock failed")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files(),
		goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(migrationLocker{SessionLocker: locker, ctx: ctx}),
		goose.WithSlog(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		return nil, errors.New("initialize embedded migrations failed")
	}
	if operation == "up" {
		if _, err = provider.Up(ctx); err != nil {
			return nil, safeError(ctx, "apply migrations; check database permissions and schema compatibility")
		}
	}
	statuses, err := provider.Status(ctx)
	if err != nil {
		return nil, safeError(ctx, "read migration status")
	}
	result := make([]MigrationStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, MigrationStatus{Version: status.Source.Version, Applied: status.State == goose.StateApplied})
	}
	return result, nil
}

// goose detaches the unlock context; preserve our original cancellation and deadline.
// If unlock is cancelled, closing this command's dedicated DB releases the session lock.
type migrationLocker struct {
	lock.SessionLocker
	ctx context.Context
}

func (l migrationLocker) SessionUnlock(_ context.Context, conn *sql.Conn) error {
	return l.SessionLocker.SessionUnlock(l.ctx, conn)
}
