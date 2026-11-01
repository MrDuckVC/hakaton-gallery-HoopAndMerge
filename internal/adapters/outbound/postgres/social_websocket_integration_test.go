package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

type socialWorld struct{ fixtureWorld }

func (socialWorld) Package(id string) (worlddomain.RoomPackage, error) {
	return worlddomain.RoomPackage{RoomID: id, Assets: []worlddomain.AssetInfo{}}, nil
}
func (socialWorld) Geometry(id string) (worlddomain.Room, error) {
	return worlddomain.Room{ID: id, Exhibits: []worlddomain.Exhibit{{ID: "ex", Position: player.Vector{X: 2, Y: 1.65}}}}, nil
}
func (socialWorld) Spawn(string) (player.State, error) {
	return player.State{WorldVersion: "v1", RoomID: "safe"}, nil
}

func TestChatAndInspectionWebSocketPostgresIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	catalog := socialWorld{}
	crypto := security.Crypto{}
	sessions := auth.New(NewAuthStore(pool), crypto, catalog, sessionevents.New(), time.Now)
	service := game.New(time.Now)
	service.Configure(catalog, NewGameStore(pool), game.NoQuestions{})
	store := NewSocialStore(pool)
	service.ConfigureSocial(store, game.UnavailablePhotos{}, crypto.NewID)
	tr := wsapi.New(service, sessions, catalog, service, []string{"http://allowed"})
	server := httptest.NewServer(tr)
	defer server.Close()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tr.Shutdown(shutdown)
	}()
	read := func(conn *ws.Conn, want string) map[string]any {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, body, err := conn.Read(bounded)
		if err != nil {
			t.Fatal(err)
		}
		var e map[string]any
		if err = json.Unmarshal(body, &e); err != nil || e["type"] != want {
			t.Fatalf("want %s: %s %v", want, body, err)
		}
		return e
	}
	connect := func(name string) (*ws.Conn, string, string) {
		t.Helper()
		login, err := sessions.Register(ctx, name, name, "password123", "")
		if err != nil {
			t.Fatal(err)
		}
		conn, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + login.Token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		read(conn, "world.snapshot")
		return conn, login.User.ID, login.Token
	}
	seq := 0
	send := func(conn *ws.Conn, kind, payload string) string {
		t.Helper()
		seq++
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", seq)
		if err := conn.Write(ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":%q,"request_id":%q,"payload":%s}`, kind, id, payload))); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, aID, _ := connect("alice")
	b, bID, _ := connect("bob")
	read(a, "player.joined")
	id := send(a, "chat.send", `{"text":"  Привет  "}`)
	first := read(a, "chat.message")
	other := read(b, "chat.message")
	p := first["payload"].(map[string]any)
	if first["request_id"] != id || other["request_id"] != nil || p["text"] != "Привет" || p["photo"] != nil || p["author"].(map[string]any)["user_id"] != aID {
		t.Fatal(first, other)
	}
	send(a, "exhibit.inspect", `{"exhibit_id":"ex"}`)
	if e := read(a, "error"); e["payload"].(map[string]any)["code"] != "GAME_NOT_READY" {
		t.Fatal(e)
	}
	send(a, "world.ready", `{"world_version":"v1","movement_epoch":1}`)
	read(a, "world.ready")
	var lastTime string
	for range 2 {
		id = send(a, "exhibit.inspect", `{"exhibit_id":"ex"}`)
		e := read(a, "exhibit.inspected")
		if e["request_id"] != id {
			t.Fatal(e)
		}
		lastTime = e["payload"].(map[string]any)["inspected_at"].(string)
	}
	events, err := store.ListMemoryEvents(ctx, aID, 10)
	if err != nil || len(events) != 1 || events[0].CreatedAt.UTC().Format(time.RFC3339Nano) != lastTime {
		t.Fatal(events, err)
	}
	events, err = store.ListMemoryEvents(ctx, bID, 10)
	if err != nil || len(events) != 0 {
		t.Fatal("leaked memory", events, err)
	}
	// Real SQL failure produces an error, never a false inspected event.
	if _, err = pool.Exec(ctx, `ALTER TABLE memory_events ADD CONSTRAINT reject_inspect CHECK(type <> 'EXHIBIT_INSPECTED') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	send(a, "exhibit.inspect", `{"exhibit_id":"ex"}`)
	if e := read(a, "error"); e["payload"].(map[string]any)["code"] != "SERVICE_UNAVAILABLE" {
		t.Fatal(e)
	}
	send(a, "chat.send", `{"text":" "}`)
	if e := read(a, "error"); e["payload"].(map[string]any)["code"] != "VALIDATION_ERROR" {
		t.Fatal(e)
	}
	send(a, "chat.send", `{"photo_id":"photo"}`)
	if e := read(a, "error"); e["payload"].(map[string]any)["code"] != "SERVICE_UNAVAILABLE" {
		t.Fatal(e)
	}
}
