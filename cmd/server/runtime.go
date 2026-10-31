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
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/postgres"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/sessionevents"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/config"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/lifecycle"
	worldcase "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/world"
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
	cancel()
	worldCtx, worldCancel := context.WithTimeout(ctx, 60*time.Second)
	catalog, err := prepareWorld(worldCtx, cfg)
	worldCancel()
	if err != nil {
		pool.Close()
		return err
	}
	listenCtx, listenCancel := context.WithTimeout(ctx, 10*time.Second)
	listener, err := (&net.ListenConfig{}).Listen(listenCtx, "tcp", cfg.HTTPAddr)
	listenCancel()
	if err != nil {
		pool.Close()
		return fmt.Errorf("open HTTP listener: %w", err)
	}
	server := httpapi.NewServer(cfg.HTTPAddr)
	sessions := sessionevents.New()
	accounts := auth.New(postgres.NewAuthStore(pool), security.Crypto{}, catalog, sessions, time.Now)
	server.ConfigureAuth(accounts, cfg.AllowedOrigins, cfg.CookieSecure)
	server.ConfigureWorld(worldcase.New(catalog, accounts, worldcase.NoConnections{}))
	return serve(ctx, server, listener, lifecycle.Coordinator{}, logger, 15*time.Second, pool.Close)
}

func prepareWorld(ctx context.Context, cfg config.Config) (*worldcase.Catalog, error) {
	catalog, err := worldcase.Load(ctx, world.Files{Dir: cfg.WorldDir}, worldwire.SnapshotSizer{}, cfg.WorldVersion)
	if err != nil {
		return nil, fmt.Errorf("prepare world: %w", err)
	}
	return catalog, nil
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
