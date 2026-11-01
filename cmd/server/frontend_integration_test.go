package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/postgres"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/config"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/testworld"
	"github.com/coder/websocket"
)

func TestFrontendSameOriginIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test and runtime database must differ")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if _, err := postgres.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	world := t.TempDir()
	if err := testworld.Write(ctx, world, "frontend_test"); err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<!doctype html><title>Local gallery</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	origin := "http://" + address
	cfg := config.Config{HTTPAddr: address, DatabaseURL: dsn, AllowedOrigins: []string{origin}, WorldDir: world, WorldVersion: "frontend_test", PhotoDir: t.TempDir(), FrontendDistDir: dist, OllamaBaseURL: "http://127.0.0.1:1", OllamaModel: "unused:test"}
	runtimeCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- run(runtimeCtx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() {
		stop()
		if runErr := <-done; runErr != nil {
			t.Error(runErr)
		}
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		request, requestErr := http.NewRequestWithContext(ctx, "GET", origin+"/", nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != 200 || !strings.Contains(string(body), "Local gallery") {
				t.Fatalf("frontend: %d %s %v", response.StatusCode, body, readErr)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("server did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	registration := `{"username":"i01_` + strings.ToLower(rand.Text())[:16] + `","display_name":"I01","password":"offline-demo-test"}`
	request, err := http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/auth/register", strings.NewReader(registration))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 201 || len(response.Cookies()) != 1 {
		t.Fatalf("registration HTTP %d", response.StatusCode)
	}
	cookie := response.Cookies()[0]
	if cookie.Secure || !cookie.HttpOnly || cookie.Name != "museum_session" {
		t.Fatal("invalid local cookie")
	}
	conn, _, err := websocket.Dial(ctx, "ws://"+address+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	_, event, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Type string }
	if err = json.Unmarshal(event, &envelope); err != nil || envelope.Type != "world.snapshot" {
		t.Fatalf("snapshot: %s %v", event, err)
	}
	for _, path := range []string{"/api/v1/me", "/api/v1/world/manifest", "/api/v1/world/rooms/r0?version=frontend_test"} {
		request, err = http.NewRequestWithContext(ctx, "GET", origin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s: HTTP %d", path, response.StatusCode)
		}
	}
}
