package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/adapters/outbound/postgres"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/config"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/testworld"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/lifecycle"
)

func TestCommandInvalidEnvironment(t *testing.T) {
	var output bytes.Buffer
	code := command(context.Background(), func(string) (string, bool) { return "", false }, &output)
	if code != 1 || !strings.Contains(output.String(), "DATABASE_URL") {
		t.Fatalf("code=%d log=%s", code, &output)
	}
}

func TestUnavailableDatabaseDoesNotExposeCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	values := map[string]string{"DATABASE_URL": "postgres://user:private-password@127.0.0.1:1/db?sslmode=disable", "ALLOWED_ORIGINS": "http://localhost:8080", "WORLD_VERSION": "v1", "OLLAMA_MODEL": "prepared:tag"}
	code := command(ctx, func(key string) (string, bool) { v, ok := values[key]; return v, ok }, &output)
	if code != 1 || strings.Contains(output.String(), "private-password") || !strings.Contains(output.String(), "PostgreSQL") {
		t.Fatalf("code=%d log=%s", code, &output)
	}
}

type notifyConnections struct{ closed chan struct{} }

func (n notifyConnections) CloseConnections(context.Context) error { close(n.closed); return nil }

type stuckPositions struct {
	started chan struct{}
	release chan struct{}
}

func (s stuckPositions) SavePositions(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}

func TestServeShutdownIsBoundedEvenWhenParticipantIgnoresContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	positions := stuckPositions{started: make(chan struct{}), release: make(chan struct{})}
	defer close(positions.release)
	closed := make(chan struct{})
	storageClosed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, httpapi.NewServer(listener.Addr().String()), listener, lifecycle.Coordinator{Positions: positions, Connections: notifyConnections{closed: closed}}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), 20*time.Millisecond, func() { close(storageClosed) })
	}()
	cancel()
	select {
	case <-positions.started:
	case <-time.After(time.Second):
		t.Fatal("shutdown not started")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown hung")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("connections cleanup was skipped")
	}
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("storage cleanup was skipped")
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, httpapi.NewServer(listener.Addr().String()), listener, lifecycle.Coordinator{}, slog.New(slog.NewTextHandler(&output, nil)), time.Second, nil)
	}()
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.Addr().String()+"/api/v1/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = response.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hung")
	}
	if !strings.Contains(output.String(), "server listening") || !strings.Contains(output.String(), "server stopped") {
		t.Fatalf("missing lifecycle logs: %s", &output)
	}
}

func TestStartupSchemaAndListenerIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test and runtime DB must differ")
	}
	// This test database is dedicated to B01. Migration is non-destructive and idempotent.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := postgres.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	dir := t.TempDir()
	if err = testworld.Write(ctx, dir, "v1"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DatabaseURL: dsn, HTTPAddr: listener.Addr().String(), WorldDir: dir, WorldVersion: "v1"}
	// World validation must fail before even an occupied listener is opened.
	if err = os.WriteFile(dir+"/catalog.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	err = run(ctx, cfg, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err == nil || !strings.Contains(err.Error(), "prepare world") {
		t.Fatalf("expected world failure before listener: %v", err)
	}
	fresh := t.TempDir()
	if err = testworld.Write(ctx, fresh, "v1"); err != nil {
		t.Fatal(err)
	}
	cfg.WorldDir = fresh
	var output bytes.Buffer
	err = run(ctx, cfg, slog.New(slog.NewTextHandler(&output, nil)))
	if err == nil || !strings.Contains(err.Error(), "HTTP listener") {
		t.Fatalf("expected occupied port error: %v", err)
	}
}

// The subprocess runs the real signal-aware entry function without a test-only production hook.
func TestServerSignalHelper(t *testing.T) {
	if os.Getenv("GALLERY_B01_SIGNAL_HELPER") != "1" {
		return
	}
	os.Exit(entry())
}

func TestSIGTERMIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test and runtime database must differ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := postgres.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = testworld.Write(ctx, dir, "signal_test"); err != nil {
		t.Fatal(err)
	}
	process := exec.CommandContext(ctx, executable, "-test.run=^TestServerSignalHelper$")
	process.Env = append(os.Environ(), "GALLERY_B01_SIGNAL_HELPER=1", "DATABASE_URL="+dsn, "HTTP_ADDR="+address, "ALLOWED_ORIGINS=http://localhost:8080", "WORLD_VERSION=signal_test", "WORLD_DIR="+dir, "OLLAMA_MODEL=unused:test", "LOG_LEVEL=info")
	stderr, err := process.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill() }()
	ready := make(chan struct{})
	logs := make(chan string, 1)
	go func() {
		var text strings.Builder
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			text.WriteString(line)
			text.WriteByte('\n')
			if strings.Contains(line, `"msg":"server listening"`) {
				close(ready)
			}
		}
		logs <- text.String()
	}()
	select {
	case <-ready:
	case text := <-logs:
		t.Fatalf("server exited before listening: %s", text)
	case <-ctx.Done():
		t.Fatal("server did not start")
	}
	if err = process.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err = process.Wait(); err != nil {
		t.Fatalf("SIGTERM exit: %v", err)
	}
	text := <-logs
	if !strings.Contains(text, `"msg":"server stopped"`) {
		t.Fatalf("missing graceful completion log: %s", text)
	}
}
