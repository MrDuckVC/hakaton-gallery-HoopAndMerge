// Command migrate applies embedded forward migrations before backend startup.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/postgres"
)

func main() { os.Exit(entry()) }

func entry() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return command(ctx, os.Args[1:], os.Getenv, os.Stdout)
}

func command(ctx context.Context, args []string, getenv func(string) string, output io.Writer) int {
	if len(args) != 1 || (args[0] != "up" && args[0] != "status") {
		_, _ = fmt.Fprintln(output, "usage: migrate up|status")
		return 1
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		_, _ = fmt.Fprintln(output, "DATABASE_URL is required")
		return 1
	}
	statuses, err := postgres.Migrate(ctx, dsn, args[0])
	if err != nil {
		_, _ = fmt.Fprintln(output, err)
		return 1
	}
	for _, status := range statuses {
		state := "pending"
		if status.Applied {
			state = "applied"
		}
		if _, err := fmt.Fprintf(output, "%d %s\n", status.Version, state); err != nil {
			return 1
		}
	}
	return 0
}
