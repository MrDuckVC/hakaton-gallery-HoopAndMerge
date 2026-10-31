package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/postgres"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/config"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/lifecycle"
)

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := postgres.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		cancel()
		return err
	}
	if err = postgres.CheckSchema(startupCtx, pool); err != nil {
		cancel()
		pool.Close()
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(startupCtx, "tcp", cfg.HTTPAddr)
	cancel()
	if err != nil {
		pool.Close()
		return fmt.Errorf("open HTTP listener: %w", err)
	}
	return serve(ctx, httpapi.NewServer(cfg.HTTPAddr), listener, lifecycle.Coordinator{}, logger, 15*time.Second, pool.Close)
}

func serve(ctx context.Context, server *httpapi.Server, listener net.Listener, participants lifecycle.Coordinator, logger *slog.Logger, timeout time.Duration, closeStorage func()) error {
	serving := make(chan error, 1)
	go func() { serving <- server.Serve(listener) }()
	logger.Info("server listening", "address", listener.Addr().String())
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-serving:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serve HTTP: %w", err)
		}
	}
	// This context must survive cancellation of the signal/startup context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	admissionErr := server.StopAccepting(shutdownCtx)
	participantErr := participants.Shutdown(shutdownCtx)
	httpErr := server.Shutdown(shutdownCtx)
	if httpErr != nil || shutdownCtx.Err() != nil {
		_ = server.Close()
		_ = listener.Close()
	}
	storageDone := make(chan struct{})
	go func() {
		if closeStorage != nil {
			closeStorage()
		}
		close(storageDone)
	}()
	var storageErr error
	select {
	case <-storageDone:
	case <-shutdownCtx.Done():
		storageErr = shutdownCtx.Err()
	}
	shutdownErr := errors.Join(participantErr, httpErr, storageErr, shutdownCtx.Err())
	if err := errors.Join(serveErr, admissionErr, shutdownErr); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}
