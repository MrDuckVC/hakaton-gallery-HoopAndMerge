package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	authcase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	guidecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
	ws "github.com/coder/websocket"
)

type profileRuntimeCatalog struct{ runtimeCatalog }

func (profileRuntimeCatalog) Profile(id string) (guide.Profile, error) {
	return guideCatalog{}.Profile(id)
}
func (profileRuntimeCatalog) Exhibit(id string) (world.Exhibit, error) {
	return guideCatalog{}.Exhibit(id)
}

func TestProfileHTTPWebSocketPublicationAndReconnectIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	catalog := profileRuntimeCatalog{}
	crypto := security.Crypto{}
	registry := game.New(time.Now)
	accounts := authcase.New(NewAuthStore(pool), crypto, catalog, sessionevents.New(), time.Now)
	store := NewRuntimeStore(pool)
	rooms := runtimecase.New(catalog, store, store, registry, runtimewire.Sizer{})
	rooms.ConfigureProfiles(catalog, store)
	if err := rooms.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	questions := guidecase.New(NewGuideStore(pool), rooms, accounts, registry, nil, guidecase.Clock{Now: time.Now, After: time.After}, crypto.NewID)
	registry.Configure(rooms, NewGameStore(pool), questions)
	registry.ConfigureRuntime(rooms)
	registry.ConfigureGuides(questions)
	transport := wsapi.New(registry, accounts, catalog, registry, []string{"http://allowed"})
	server := httpapi.NewServer("")
	server.ConfigureAuth(accounts, []string{"http://allowed"}, false)
	server.ConfigureRuntime(rooms, accounts, []string{"http://allowed"})
	server.ConfigureAdminGuides(rooms)
	server.ConfigureWebSocket(transport)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	defer func() {
		bounded, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = transport.Shutdown(bounded)
	}()
	read := func(c *ws.Conn, want string) runtimeTestEvent {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, body, err := c.Read(bounded)
		if err != nil {
			t.Fatal(err)
		}
		var e runtimeTestEvent
		if err = json.Unmarshal(body, &e); err != nil || e.Type != want {
			t.Fatalf("want %s got %s: %v", want, body, err)
		}
		return e
	}
	connect := func(token string) *ws.Conn {
		t.Helper()
		c, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.CloseNow() })
		read(c, "world.snapshot")
		return c
	}
	alice, err := accounts.Register(ctx, "profiles_alice", "Alice", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := accounts.Register(ctx, "profiles_bob", "Bob", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.GrantAdmin(ctx, alice.User.Username); err != nil {
		t.Fatal(err)
	}
	a := connect(alice.Token)
	read(a, "world.runtime")
	b := connect(bob.Token)
	read(b, "world.runtime")
	read(a, "player.joined")
	// Both sockets remain not-ready; profile changes still reach both.
	body := `{"world_version":"v1","expected_revision":1,"settings":{"display_name":"Новое имя","subtitle":"Новая подпись","personality":"P","style":"S","topic":"T","constraints":["C"],"material_ids":[]}}`
	put := func(body string) int {
		t.Helper()
		req, e := http.NewRequestWithContext(ctx, http.MethodPut, httpServer.URL+"/api/v1/admin/guides/guide", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Origin", "http://allowed")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "museum_session", Value: alice.Token})
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = res.Body.Close() }()
		data, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		if res.StatusCode == 200 && !bytes.Contains(data, []byte(`"revision":2`)) {
			t.Fatal(string(data))
		}
		return res.StatusCode
	}
	if status := put(body); status != 200 {
		t.Fatal(status)
	}
	for _, c := range []*ws.Conn{a, b} {
		event := read(c, "world.runtime")
		var room runtimewire.Room
		if err = json.Unmarshal(event.Payload, &room); err != nil || room.Revision != 2 || room.Guides[0].DisplayName != "Новое имя" || room.Guides[0].Subtitle != "Новая подпись" {
			t.Fatal(room, err)
		}
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE room_overlays ADD CONSTRAINT reject_profile_revision CHECK(revision<3)`); err != nil {
		t.Fatal(err)
	}
	failed := strings.Replace(body, `"expected_revision":1`, `"expected_revision":2`, 1)
	failed = strings.Replace(failed, "Новое имя", "Запрещённое имя", 1)
	if status := put(failed); status != 503 {
		t.Fatal(status)
	}
	// A correlated sync must be next: SQL failure cannot publish partial state.
	for _, c := range []*ws.Conn{a, b} {
		id := crypto.NewID()
		command, _ := json.Marshal(map[string]any{"type": "room.sync", "request_id": id, "payload": map[string]any{}})
		if err = c.Write(ctx, ws.MessageText, command); err != nil {
			t.Fatal(err)
		}
		e := read(c, "world.runtime")
		if e.RequestID != id || bytes.Contains(e.Payload, []byte("Запрещённое имя")) {
			t.Fatal(string(e.Payload))
		}
	}
	// Replacement and a new service both resolve the same durable public identity.
	replacement := connect(alice.Token)
	e := read(replacement, "world.runtime")
	if !bytes.Contains(e.Payload, []byte("Новое имя")) {
		t.Fatal(string(e.Payload))
	}
	restarted := runtimecase.New(catalog, NewRuntimeStore(pool), store, registry, runtimewire.Sizer{})
	restarted.ConfigureProfiles(catalog, store)
	if err = restarted.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Read(ctx, "safe")
	if err != nil || restored.Guides[0].DisplayName != "Новое имя" || restored.Revision != 2 {
		t.Fatal(restored, err)
	}
}
