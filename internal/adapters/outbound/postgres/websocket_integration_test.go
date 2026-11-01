package postgres

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	worldfiles "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/testworld"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
	ws "github.com/coder/websocket"
)

func TestWebSocketWorldAndLogoutIntegration(t *testing.T) {
	dir := t.TempDir()
	if err := testworld.Write(t.Context(), dir, "v1"); err != nil {
		t.Fatal(err)
	}
	catalog, err := world.Load(t.Context(), worldfiles.Files{Dir: dir}, worldwire.SnapshotSizer{}, "v1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, dsn, pool := testDatabase(t)
	if _, err = Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	sessions := auth.New(NewAuthStore(pool), security.Crypto{}, catalog, sessionevents.New(), time.Now)
	connections := game.New(time.Now)
	transport := wsapi.New(connections, sessions, catalog, game.PendingHandler{}, []string{"http://allowed"})
	api := httpapi.NewServer(":0")
	api.ConfigureAuth(sessions, []string{"http://allowed"}, false)
	api.ConfigureWorld(world.New(catalog, sessions, connections))
	api.ConfigureWebSocket(transport)
	server := httptest.NewServer(api.Handler)
	defer server.Close()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = transport.Shutdown(shutdownCtx)
	}()
	request := func(method, path, body string, cookie *http.Cookie, want int) *http.Response {
		t.Helper()
		req, e := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Origin", "http://allowed")
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		if response.StatusCode != want {
			b, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			t.Fatalf("%s: status=%d want=%d body=%s", path, response.StatusCode, want, b)
		}
		return response
	}
	response := request("POST", "/api/v1/auth/register", `{"username":"alice","display_name":"Alice","password":"password123"}`, nil, 201)
	_ = response.Body.Close()
	login := func() *http.Cookie {
		t.Helper()
		r := request("POST", "/api/v1/auth/login", `{"username":"alice","password":"password123"}`, nil, 200)
		defer func() { _ = r.Body.Close() }()
		return r.Cookies()[0]
	}
	first := login()
	r := request("GET", "/api/v1/world/rooms/r0?version=v1", "", first, 409)
	_ = r.Body.Close()
	connect := func(cookie *http.Cookie) *ws.Conn {
		t.Helper()
		conn, _, e := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {cookie.String()}}})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		_, body, e := conn.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var snapshot worldwire.SnapshotEnvelope
		if e = json.Unmarshal(body, &snapshot); e != nil {
			t.Fatal(e)
		}
		if snapshot.Type != "world.snapshot" || snapshot.Payload.Self.RoomID != "r0" || snapshot.Payload.CurrentRoom.RoomID != "r0" || snapshot.Payload.Self.MovementEpoch != 1 {
			t.Fatalf("snapshot: %s", body)
		}
		return conn
	}
	old := connect(first)
	r = request("GET", "/api/v1/world/rooms/r0?version=v1", "", first, 200)
	_ = r.Body.Close()
	r = request("GET", "/api/v1/world/assets/a0?version=v1", "", first, 200)
	_ = r.Body.Close()
	second := login()
	r = request("GET", "/api/v1/world/rooms/r0?version=v1", "", second, 409)
	_ = r.Body.Close()
	current := connect(second)
	_, _, err = old.Read(ctx)
	if ws.CloseStatus(err) != 4009 {
		t.Fatal(err)
	}
	r = request("GET", "/api/v1/world/rooms/r0?version=v1", "", first, 409)
	_ = r.Body.Close()
	r = request("GET", "/api/v1/world/rooms/r0?version=v1", "", second, 200)
	_ = r.Body.Close()
	r = request("POST", "/api/v1/auth/logout", "", second, 204)
	_ = r.Body.Close()
	_, _, err = current.Read(ctx)
	if ws.CloseStatus(err) != 4001 {
		t.Fatal(err)
	}
	r = request("GET", "/api/v1/world/rooms/r0?version=v1", "", second, 401)
	_ = r.Body.Close()
}
