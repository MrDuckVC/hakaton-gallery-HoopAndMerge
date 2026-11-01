package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/postgres"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/config"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/testworld"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
)

type sundayEvent struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

type sundayClient struct {
	t      *testing.T
	ctx    context.Context
	origin string
	cookie *http.Cookie
	ws     *websocket.Conn
}

func (c sundayClient) http(method, path string, body any, status int) map[string]any {
	c.t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(c.ctx, method, c.origin+"/api/v1"+path, bytes.NewReader(data))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Origin", c.origin)
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var value map[string]any
	if err = json.NewDecoder(res.Body).Decode(&value); err != nil {
		c.t.Fatal(err)
	}
	if res.StatusCode != status {
		c.t.Fatalf("%s %s: HTTP %d, want %d: %v", method, path, res.StatusCode, status, value)
	}
	return value
}

func (c sundayClient) send(kind string, payload any) string {
	c.t.Helper()
	id := security.Crypto{}.NewID()
	e := map[string]any{"type": kind, "payload": payload}
	if kind != "client.metrics" && kind != "telemetry.pong" {
		e["request_id"] = id
	}
	data, err := json.Marshal(e)
	if err != nil {
		c.t.Fatal(err)
	}
	if err = c.ws.Write(c.ctx, websocket.MessageText, data); err != nil {
		c.t.Fatal(err)
	}
	return id
}

func (c sundayClient) read(kind, request string) map[string]any {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	defer cancel()
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			c.t.Fatalf("read %s: %v", kind, err)
		}
		var event sundayEvent
		if err = json.Unmarshal(data, &event); err != nil {
			c.t.Fatal(err)
		}
		var payload map[string]any
		if err = json.Unmarshal(event.Payload, &payload); err != nil {
			c.t.Fatal(err)
		}
		if event.Type == "telemetry.probe" {
			c.send("telemetry.pong", payload)
		}
		if event.Type == kind && (request == "" || event.RequestID == request) {
			return payload
		}
		if event.Type == "error" {
			c.t.Fatalf("unexpected error waiting for %s: %s", kind, data)
		}
	}
}

// Exercise the actual composition root, rather than rebuilding its dependency graph in a test.
func TestSundayCompositionIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	if dsn == os.Getenv("DATABASE_URL") {
		t.Fatal("test and runtime database must differ")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	dsn = sundayDatabase(ctx, t, dsn)
	if _, err := postgres.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	origin := "http://" + address
	dist := t.TempDir()
	if supplied := os.Getenv("SUNDAY_TEST_FRONTEND_DIST"); supplied != "" {
		dist = supplied
	} else if err = os.WriteFile(filepath.Join(dist, "index.html"), []byte("<!doctype html><title>Sunday test</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	worldDir, version := sundayWorld(ctx, t)
	cfg := config.Config{HTTPAddr: address, DatabaseURL: dsn, AllowedOrigins: []string{origin}, WorldDir: worldDir, WorldVersion: version, PhotoDir: t.TempDir(), FrontendDistDir: dist, OllamaBaseURL: "http://127.0.0.1:1", OllamaModel: "unused:test"}
	var stop context.CancelFunc
	var done chan error
	start := func() {
		runtimeCtx, stopRuntime := context.WithCancel(ctx)
		stop = stopRuntime
		done = make(chan error, 1)
		go func() { done <- run(runtimeCtx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
		for {
			req, requestErr := http.NewRequestWithContext(ctx, "GET", origin+"/", nil)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			res, requestErr := http.DefaultClient.Do(req)
			if requestErr == nil {
				_ = res.Body.Close()
				if res.StatusCode != 200 {
					t.Fatal(res.StatusCode)
				}
				break
			}
			select {
			case runErr := <-done:
				stop()
				stop = nil
				t.Fatalf("startup: %v", runErr)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	shutdown := func() {
		if stop != nil {
			stop()
			runErr := <-done
			stop = nil
			if runErr != nil {
				t.Error(runErr)
			}
		}
	}
	defer shutdown()
	start()
	register := func() (sundayClient, string) {
		username := "s07_" + strings.ToLower(rand.Text())[:16]
		data, _ := json.Marshal(map[string]string{"username": username, "display_name": "Sunday", "password": "sunday-test-only"})
		req, requestErr := http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/auth/register", bytes.NewReader(data))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		res, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_ = res.Body.Close()
		if res.StatusCode != 201 || len(res.Cookies()) != 1 {
			t.Fatal("registration", res.StatusCode)
		}
		return sundayClient{t: t, ctx: ctx, origin: origin, cookie: res.Cookies()[0]}, username
	}
	attach := func(c *sundayClient) map[string]any {
		conn, _, dialErr := websocket.Dial(ctx, "ws://"+address+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {c.cookie.String()}}})
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		c.ws = conn
		t.Cleanup(func() { _ = conn.CloseNow() })
		// Assert the first three events without filtering: no tick/probe may overtake attach.
		var runtime map[string]any
		for _, want := range []string{"world.snapshot", "world.runtime", "npc.state"} {
			_, data, readErr := conn.Read(ctx)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var e sundayEvent
			if readErr = json.Unmarshal(data, &e); readErr != nil || e.Type != want {
				t.Fatalf("attach want %s: %s (%v)", want, data, readErr)
			}
			var p map[string]any
			if readErr = json.Unmarshal(e.Payload, &p); readErr != nil {
				t.Fatal(readErr)
			}
			if want == "world.snapshot" && p["protocol_version"] != float64(1) {
				t.Fatal(p)
			}
			if want == "world.runtime" {
				runtime = p
			}
			if want == "npc.state" && len(p["npcs"].([]any)) != 10 {
				t.Fatal(p)
			}
		}
		return runtime
	}
	a, username := register()
	b, _ := register()
	sundayClient{t: t, ctx: ctx, origin: origin}.http("GET", "/admin/me", nil, 401)
	a.http("GET", "/admin/me", nil, 403)
	if err = postgres.NewRuntimeStore(pool).GrantAdmin(ctx, username); err != nil {
		t.Fatal(err)
	}
	a.http("GET", "/admin/me", nil, 200)
	manifest := a.http("GET", "/world/manifest", nil, 200)
	extensions, ok := manifest["extensions"].([]any)
	if !ok || len(extensions) != 1 || extensions[0] != "sunday_v1" {
		t.Fatal("Sunday capability", manifest)
	}
	runtime := attach(&a)
	attach(&b)
	roomID := runtime["room_id"].(string)
	configBody := runtime["config"].(map[string]any)
	configBody["lighting"] = "cool"
	// Both lamp slots must be reachable from the (6,6) start without changing geometry.
	slots := configBody["lamp_slots"].([]any)
	slots[0].(map[string]any)["position"] = map[string]any{"x": 4, "y": 0, "z": 5}
	slots[1].(map[string]any)["position"] = map[string]any{"x": 8, "y": 0, "z": 5}
	update := map[string]any{"world_version": cfg.WorldVersion, "expected_revision": runtime["revision"], "config": configBody}
	runtime = a.http("PUT", "/admin/rooms/"+roomID, update, 200)
	a.http("PUT", "/admin/rooms/"+roomID, update, 409)
	a.read("world.runtime", "")
	b.read("world.runtime", "")
	act := func(c sundayClient, action string, extra map[string]any) string {
		p := map[string]any{"object_id": "lamp_01", "action": action, "movement_epoch": 1, "expected_revision": runtime["revision"]}
		for k, v := range extra {
			p[k] = v
		}
		return c.send("object.act", p)
	}
	id := act(a, "take", nil)
	if e := a.read("error", id); e["code"] != "GAME_NOT_READY" {
		t.Fatal(e)
	}
	// Chat and telemetry remain available before ready.
	id = a.send("chat.send", map[string]any{"text": "Sunday integration"})
	a.read("chat.message", id)
	a.send("client.metrics", map[string]any{"fps": 60, "visible": true})
	b.send("client.metrics", map[string]any{"fps": nil, "visible": false})
	for _, c := range []sundayClient{a, b} {
		id = c.send("world.ready", map[string]any{"world_version": cfg.WorldVersion, "movement_epoch": 1})
		c.read("world.ready", id)
	}
	id = act(a, "take", nil)
	runtime = a.read("object.acted", id)["runtime"].(map[string]any)
	id = act(b, "take", nil)
	if e := b.read("error", id); e["code"] != "OBJECT_BUSY" {
		t.Fatal(e)
	}
	id = act(a, "place", map[string]any{"slot_id": "b"})
	runtime = a.read("object.acted", id)["runtime"].(map[string]any)
	id = act(a, "aim", map[string]any{"yaw": 0.5, "head_pitch": 0.2})
	runtime = a.read("object.acted", id)["runtime"].(map[string]any)
	id = act(a, "power", map[string]any{"on": false})
	runtime = a.read("object.acted", id)["runtime"].(map[string]any)
	id = a.send("guide.action", map[string]any{"guide_id": "guide_leonardo", "text": "покажи макет", "movement_epoch": 1, "expected_revision": runtime["revision"]})
	runtime = a.read("guide.action.completed", id)["runtime"].(map[string]any)
	if runtime["config"].(map[string]any)["installation_visible"] != true {
		t.Fatal(runtime)
	}
	profile := a.http("GET", "/admin/guides/guide_leonardo", nil, 200)
	settings := profile["settings"].(map[string]any)
	settings["display_name"] = username
	a.http("PUT", "/admin/guides/guide_leonardo", map[string]any{"world_version": cfg.WorldVersion, "expected_revision": profile["revision"], "settings": settings}, 200)
	// Consume through the scheduled challenge, replying with the real nonce.
	a.read("telemetry.probe", "")
	id = a.send("room.sync", map[string]any{})
	a.read("world.runtime", id)
	a.read("npc.state", id)
	clients := a.http("GET", "/admin/clients", nil, 200)["clients"].([]any)
	if len(clients) != 2 {
		t.Fatal(clients)
	}
	var hasPing bool
	for _, value := range clients {
		c := value.(map[string]any)
		if c["ping_ms"] != nil {
			hasPing = true
		}
	}
	if !hasPing {
		t.Fatal("pong did not update admin clients", clients)
	}
	// Shutdown with a held lamp, then reattach using the original session.
	id = act(a, "take", nil)
	// Profile publication raised the room revision: stale CAS must fail before retry.
	if e := a.read("error", id); e["code"] != "REVISION_CONFLICT" {
		t.Fatal(e)
	}
	runtime = a.http("GET", "/admin/rooms/"+roomID, nil, 200)
	id = act(a, "take", nil)
	a.read("object.acted", id)
	shutdown()
	start()
	runtime = attach(&a)
	if runtime["lamp"].(map[string]any)["holder_user_id"] != nil || runtime["lamp"].(map[string]any)["on"] != false || runtime["config"].(map[string]any)["installation_visible"] != true {
		t.Fatal("runtime not restored", runtime)
	}
	guides := runtime["guides"].([]any)
	if guides[0].(map[string]any)["display_name"] != username {
		t.Fatal("profile not restored", runtime)
	}
	id = a.send("room.sync", map[string]any{})
	a.read("world.runtime", id)
	a.read("npc.state", id)
}

// Own all test data so existing C01 overrides and simultaneous runs cannot interfere.
func sundayDatabase(ctx context.Context, t *testing.T, dsn string) string {
	t.Helper()
	admin, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "s07_" + strings.ToLower(rand.Text())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defer admin.Close()
		if _, cleanupErr := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// Keep instrumented CI independent of decoding the full 318 MB C01 within startup's deadline.
// Operators can opt into that real catalog in an uninstrumented acceptance run.
func sundayWorld(ctx context.Context, t *testing.T) (string, string) {
	t.Helper()
	if dir := os.Getenv("SUNDAY_TEST_WORLD_DIR"); dir != "" {
		return dir, "gallery_c01_v1"
	}
	dir := t.TempDir()
	const version = "sunday_test_v1"
	if err := testworld.Write(ctx, dir, version); err != nil {
		t.Fatal(err)
	}
	read := func(name string) map[string]any {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err = json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	write := func(name string, value map[string]any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	catalog := read("catalog.json")
	catalog["start"].(map[string]any)["position"] = map[string]any{"x": 6, "y": 0, "z": 6}
	catalog["profiles"] = []any{map[string]any{"id": "guide_leonardo", "file": "profile.json"}}
	write("catalog.json", catalog)
	room := read("r0.json")
	room["guides"] = []any{map[string]any{"id": "guide_leonardo", "display_name": "Test guide", "subtitle": "Educational reconstruction", "position": map[string]any{"x": 6, "y": 0, "z": 7.5}, "rotation_y": 0, "interaction_radius_m": 3}}
	write("r0.json", room)
	write("profile.json", map[string]any{"personality": "Educational test reconstruction", "style": "Simple", "topic": "Art", "constraints": []string{"Use prepared materials"}, "materials": []any{}})
	return dir, version
}
