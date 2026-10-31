// Command server is the composition root of the gallery backend.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/config"
)

func main() { os.Exit(entry()) }

func entry() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return command(ctx, os.LookupEnv, os.Stderr)
}

func command(ctx context.Context, lookup func(string) (string, bool), output io.Writer) int {
	cfg, err := config.Load(lookup)
	if err != nil {
		slog.New(slog.NewJSONHandler(output, nil)).Error("invalid configuration", "error", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("server failed", "error", err)
		return 1
	}
	return 0
}
