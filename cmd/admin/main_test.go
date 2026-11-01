package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"grant"}, {"grant", "--username"}, {"grant", "--username", "alice", "extra"}, {"revoke", "--username", "alice"}, {"grant", "--username", "a"}, {"grant", "--username", "ali ce"}} {
		var out bytes.Buffer
		if code := command(context.Background(), args, func(string) string { return "" }, &out); code != 1 {
			t.Fatalf("args %v: %d", args, code)
		}
	}
}

func TestGrantCommandIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test and runtime database must differ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := "admin_cli_" + time.Now().Format("150405000000000")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, e := pool.Exec(cleanup, "DROP SCHEMA "+identifier+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	isolated := dsn + sep + "search_path=" + schema
	if _, err = postgres.Migrate(ctx, isolated, "up"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO "+identifier+".users(id,username,display_name,avatar_color,password_hash) VALUES('00000000-0000-4000-8000-000000000037','alice','Alice','#AABBCC','encoded')"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		username string
		want     int
	}{{"missing", 1}, {"Alice", 0}, {"ALICE", 0}} {
		var output bytes.Buffer
		if code := command(ctx, []string{"grant", "--username", tc.username}, func(string) string { return isolated }, &output); code != tc.want {
			t.Fatalf("username=%s code=%d output=%s", tc.username, code, &output)
		}
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+identifier+".admin_memberships").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestCommandDoesNotLeakDatabaseURL(t *testing.T) {
	var out bytes.Buffer
	code := command(context.Background(), []string{"grant", "--username", "alice"}, func(string) string { return "postgres://private-password@bad host/db" }, &out)
	if code != 1 || strings.Contains(out.String(), "private-password") {
		t.Fatalf("code %d: %s", code, &out)
	}
}

func TestCommandMissingDatabaseAndGrantResults(t *testing.T) {
	var out bytes.Buffer
	if code := command(context.Background(), []string{"grant", "--username", "Alice"}, func(string) string { return "" }, &out); code != 1 {
		t.Fatal(code)
	}
	for _, grantErr := range []error{nil, errors.New("failed")} {
		out.Reset()
		called := false
		code := runGrant(context.Background(), "Alice", &out, func(_ context.Context, name string) error {
			called = true
			if name != "alice" {
				t.Fatal(name)
			}
			return grantErr
		})
		if !called || (code == 0) != (grantErr == nil) {
			t.Fatalf("code %d, called %v", code, called)
		}
	}
}
