package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Every test owns a random schema in an explicitly supplied test database.
func testDatabase(t *testing.T) (context.Context, string, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; isolated PostgreSQL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("TEST_DATABASE_URL must differ from DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("invalid test DB configuration")
	}
	schema := "b01_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_")) + "_" + time.Now().Format("150405000000000")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatal("create isolated test schema: ", err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	isolatedDSN := cfg.ConnConfig.ConnString()
	// ConnString retains the original URL; append a query parameter explicitly.
	separator := "?"
	if strings.Contains(isolatedDSN, "?") {
		separator = "&"
	}
	isolatedDSN += separator + "search_path=" + schema
	pool, err := pgxpool.New(ctx, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error("drop owned test schema: ", err)
		}
		admin.Close()
	})
	return ctx, isolatedDSN, pool
}

func TestMigrationAndStartupIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if err := CheckSchema(ctx, pool); err == nil {
		t.Fatal("empty schema accepted")
	}
	var tables int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("startup modified schema: tables=%d error=%v", tables, err)
	}
	status, err := Migrate(ctx, dsn, "up")
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 6 {
		t.Fatalf("migration status: %v", status)
	}
	if err = CheckSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
	const userID = "00000000-0000-4000-8000-000000000001"
	if _, err := pool.Exec(ctx, "INSERT INTO users (id,username,display_name,avatar_color,password_hash) VALUES ($1,'alice','Alice','#AABBCC','encoded-kdf-with-salt')", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO sessions (id, token_hash,user_id,expires_at) VALUES ('00000000-0000-4000-8000-000000000002',decode(repeat('ab',32),'hex'),$1,now()+interval '24 hours')", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO player_states (user_id,world_version,room_id,x,y,z,yaw,pitch) VALUES ($1,'world_v1','lobby',1,0,-2,0,0)", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "sessions", "player_states"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s data not preserved: %d, %v", table, count, err)
		}
	}
	if _, err := Migrate(ctx, dsn, "status"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO users (id,username,display_name,avatar_color,password_hash) VALUES ('00000000-0000-4000-8000-000000000003','alice','Duplicate','#FFFFFF','kdf')",
		"UPDATE users SET username='Alice'", "UPDATE users SET display_name=''", "UPDATE users SET avatar_color='red'",
		"UPDATE sessions SET token_hash=decode('ab','hex')",
		"UPDATE sessions SET user_id='00000000-0000-4000-8000-000000000099'",
		"UPDATE player_states SET y=1", "UPDATE player_states SET x='NaN'", "UPDATE player_states SET z='Infinity'",
		"UPDATE player_states SET yaw=4", "UPDATE player_states SET pitch=-2", "UPDATE player_states SET room_id=''",
	} {
		if _, err := pool.Exec(ctx, query); err == nil {
			t.Errorf("constraint missing for %s", query)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO goose_db_version (version_id,is_applied) VALUES (7,true)"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSchema(ctx, pool); err == nil {
		t.Fatal("newer schema accepted")
	}
}

func TestSchemaCheckReadOnlyIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		t.Fatal(err)
	}
	// Single acquired session ensures read-only applies to the schema check.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSchema(ctx, conn); err != nil {
		t.Fatal(err)
	}
}
