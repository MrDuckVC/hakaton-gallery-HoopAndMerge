// Command admin grants administrator membership to an existing gallery account.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/postgres"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
)

func main() { os.Exit(entry()) }

func entry() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return command(ctx, os.Args[1:], os.Getenv, os.Stdout)
}

func command(ctx context.Context, args []string, getenv func(string) string, output io.Writer) int {
	if len(args) != 3 || args[0] != "grant" || args[1] != "--username" {
		_, _ = fmt.Fprintln(output, "usage: admin grant --username NAME")
		return 1
	}
	username, _, err := auth.Credentials(args[2], "", "validation-only", false)
	if err != nil {
		_, _ = fmt.Fprintln(output, "invalid username")
		return 1
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		_, _ = fmt.Fprintln(output, "DATABASE_URL is required")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		_, _ = fmt.Fprintln(output, "database unavailable; check DATABASE_URL")
		return 1
	}
	defer pool.Close()
	if err = postgres.CheckSchema(ctx, pool); err != nil {
		_, _ = fmt.Fprintln(output, "apply migrations separately with make migrate-up before granting membership")
		return 1
	}
	return runGrant(ctx, username, output, postgres.NewRuntimeStore(pool).GrantAdmin)
}

func runGrant(ctx context.Context, username string, output io.Writer, grant func(context.Context, string) error) int {
	if err := grant(ctx, strings.ToLower(username)); err != nil {
		_, _ = fmt.Fprintln(output, "grant failed; account must exist and database must be available")
		return 1
	}
	if _, err := fmt.Fprintln(output, "administrator membership granted"); err != nil {
		return 1
	}
	return 0
}
