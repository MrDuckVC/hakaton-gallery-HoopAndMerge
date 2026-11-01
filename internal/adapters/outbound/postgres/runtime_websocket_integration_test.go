package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	authcase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	guidecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
	ws "github.com/coder/websocket"
)

type runtimeCatalog struct{}

func (runtimeCatalog) Manifest() world.Manifest { return world.Manifest{ID: "test", Version: "v1"} }
func (runtimeCatalog) Rooms() []world.Room {
	return []world.Room{{ID: "safe", Version: "v1", Title: "Runtime test", Max: player.Vector{X: 12, Y: 4, Z: 12}, Doors: []world.Door{{ID: "exit", Target: "safe", Position: player.Vector{X: 11, Z: 11}, Radius: 1.5}}, Guides: []world.Guide{{ID: "guide", DisplayName: "Guide", Subtitle: "Test reconstruction", Position: player.Vector{X: 6, Z: 7.5}, Radius: 3}}}}
}
func (c runtimeCatalog) Geometry(id string) (world.Room, error) {
	if id != "safe" {
		return world.Room{}, world.NotFound
	}
	return c.Rooms()[0], nil
}
func (runtimeCatalog) SpawnPositions(string) []player.Vector { return []player.Vector{{X: 2, Z: 2}} }
func (runtimeCatalog) HasGuide(id string) bool               { return id == "guide" }
func (runtimeCatalog) Package(id string) (world.RoomPackage, error) {
	if id != "safe" {
		return world.RoomPackage{}, world.NotFound
	}
	return world.RoomPackage{RoomID: id, Assets: []world.AssetInfo{}}, nil
}
func (runtimeCatalog) Spawn(string) (player.State, error) {
	return player.State{WorldVersion: "v1", RoomID: "safe", Position: player.Vector{X: 2, Z: 2}}, nil
}
func (c runtimeCatalog) Start(context.Context) (player.State, error) { return c.Spawn("") }
func (c runtimeCatalog) Valid(_ context.Context, p player.State) (bool, error) {
	r, _ := c.Geometry(p.RoomID)
	return p.RoomID == "safe" && p.WorldVersion == "v1" && world.ValidPose(r, p), nil
}

type runtimeTestEvent struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

func TestRuntimeHTTPWebSocketDurabilityIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	catalog := runtimeCatalog{}
	crypto := security.Crypto{}
	accounts := authcase.New(NewAuthStore(pool), crypto, catalog, sessionevents.New(), time.Now)
	registry := game.New(time.Now)
	store := NewRuntimeStore(pool)
	runtime := runtimecase.New(catalog, store, store, registry, runtimewire.Sizer{})
	if err := runtime.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	questions := guidecase.New(NewGuideStore(pool), runtime, accounts, registry, nil, guidecase.Clock{Now: time.Now, After: time.After}, crypto.NewID)
	registry.Configure(runtime, NewGameStore(pool), questions)
	registry.ConfigureRuntime(runtime)
	if err := registry.ConfigureNPCs(catalog.Rooms(), crypto.NewID); err != nil {
		t.Fatal(err)
	}
	registry.ConfigureGuides(questions)
	transport := wsapi.New(registry, accounts, catalog, registry, []string{"http://allowed"})
	server := httpapi.NewServer("")
	server.ConfigureAuth(accounts, []string{"http://allowed"}, false)
	server.ConfigureRuntime(runtime, accounts, []string{"http://allowed"})
	server.ConfigureWebSocket(transport)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = transport.Shutdown(shutdown)
	}()
	read := func(conn *ws.Conn, want string) runtimeTestEvent {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, body, err := conn.Read(bounded)
		if err != nil {
			t.Fatal(err)
		}
		var event runtimeTestEvent
		if err = json.Unmarshal(body, &event); err != nil || event.Type != want {
			t.Fatalf("want %s: %s (%v)", want, body, err)
		}
		return event
	}
	connect := func(token string) *ws.Conn {
		t.Helper()
		conn, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		read(conn, "world.snapshot")
		event := read(conn, "world.runtime")
		var initial runtimewire.Room
		if err = json.Unmarshal(event.Payload, &initial); err != nil || initial.Revision != 1 {
			t.Fatal(initial, err)
		}
		npcEvent := read(conn, "npc.state")
		var npcs struct {
			Seq  int64             `json:"seq"`
			NPCs []json.RawMessage `json:"npcs"`
		}
		if err = json.Unmarshal(npcEvent.Payload, &npcs); err != nil || npcs.Seq < 1 || len(npcs.NPCs) != 5 {
			t.Fatal(string(npcEvent.Payload), err)
		}
		return conn
	}
	alice, err := accounts.Register(ctx, "runtime_alice", "Alice", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := accounts.Register(ctx, "runtime_bob", "Bob", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.GrantAdmin(ctx, alice.User.Username); err != nil {
		t.Fatal(err)
	}
	a, b := connect(alice.Token), connect(bob.Token)
	read(a, "player.joined")
	session, _, err := accounts.Authenticate(ctx, alice.Token)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := catalog.Start(ctx)
	// The immutable placement is too far away; after editing the same ask must be accepted.
	if f := questions.Ask(ctx, guidecase.Input{Session: session, State: state, Ready: true}, crypto.NewID(), "guide", "hello", "", func() guidecase.Failure { return guidecase.Failure{} }); f.Code != "OUT_OF_RANGE" {
		t.Fatal(f)
	}
	config := domain.Seed(catalog.Rooms()[0]).Config
	config.Lighting = "warm"
	config.NPCCount = 0
	config.GuidePlacements[0].Position = player.Vector{X: 2, Z: 4.5}
	publicConfig := runtimewire.Public(domain.Room{Overlay: domain.Overlay{Config: config}}).Config
	put := func(revision int64) (int, []byte, error) {
		body, e := json.Marshal(struct {
			Version  string             `json:"world_version"`
			Revision int64              `json:"expected_revision"`
			Config   runtimewire.Config `json:"config"`
		}{"v1", revision, publicConfig})
		if e != nil {
			return 0, nil, e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPut, httpServer.URL+"/api/v1/admin/rooms/safe", bytes.NewReader(body))
		if e != nil {
			return 0, nil, e
		}
		req.Header.Set("Origin", "http://allowed")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "museum_session", Value: alice.Token})
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			return 0, nil, e
		}
		defer func() { _ = res.Body.Close() }()
		data, e := io.ReadAll(res.Body)
		return res.StatusCode, data, e
	}
	type putResult struct {
		status int
		body   []byte
		err    error
	}
	results := make(chan putResult, 2)
	for range 2 {
		go func() { status, body, e := put(1); results <- putResult{status, body, e} }()
	}
	success, conflict := 0, 0
	var committed runtimewire.Room
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		switch result.status {
		case 200:
			success++
			if err = json.Unmarshal(result.body, &committed); err != nil {
				t.Fatal(err)
			}
		case 409:
			conflict++
			if !bytes.Contains(result.body, []byte("REVISION_CONFLICT")) {
				t.Fatal(string(result.body))
			}
		default:
			t.Fatal(result.status, string(result.body))
		}
	}
	if success != 1 || conflict != 1 || committed.Revision != 2 {
		t.Fatal(success, conflict, committed)
	}
	for _, conn := range []*ws.Conn{a, b} {
		event := read(conn, "world.runtime")
		var got runtimewire.Room
		if err = json.Unmarshal(event.Payload, &got); err != nil || event.RequestID != "" || !reflect.DeepEqual(got, committed) {
			t.Fatalf("broadcast=%s, committed=%+v err=%v", event.Payload, committed, err)
		}
	}
	for _, conn := range []*ws.Conn{a, b} {
		npcEvent := read(conn, "npc.state")
		var npcs struct {
			NPCs []json.RawMessage `json:"npcs"`
		}
		if err = json.Unmarshal(npcEvent.Payload, &npcs); err != nil || npcs.NPCs == nil || len(npcs.NPCs) != 0 {
			t.Fatal(string(npcEvent.Payload), err)
		}
	}
	if committed.Guides[0].Position.X != 2 || committed.Guides[0].Position.Z != 4.5 {
		t.Fatal(committed.Guides)
	}
	restarted := runtimecase.New(catalog, NewRuntimeStore(pool), store, registry, runtimewire.Sizer{})
	if err = restarted.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Read(ctx, "safe")
	if err != nil || !reflect.DeepEqual(runtimewire.Public(restored), committed) {
		t.Fatal(restored, err)
	}
	send := func(conn *ws.Conn, kind, payload string) string {
		t.Helper()
		id := crypto.NewID()
		if e := conn.Write(ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":%q,"request_id":%q,"payload":%s}`, kind, id, payload))); e != nil {
			t.Fatal(e)
		}
		return id
	}
	// Ready + Ask goes through the real WS handler and uses the effective runtime geometry.
	send(a, "world.ready", `{"world_version":"v1","movement_epoch":1}`)
	read(a, "world.ready")
	askID := send(a, "guide.ask", `{"guide_id":"guide","text":"hello"}`)
	if event := read(a, "guide.accepted"); event.RequestID != askID {
		t.Fatal(event)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE room_overlays ADD CONSTRAINT reject_next_revision CHECK(revision < 3)`); err != nil {
		t.Fatal(err)
	}
	status, body, err := put(2)
	if err != nil || status != 503 || !bytes.Contains(body, []byte("SERVICE_UNAVAILABLE")) {
		t.Fatalf("SQL failure status=%d body=%s err=%v", status, body, err)
	}
	// A correlated sync is the next event on each socket: failed SQL published nothing.
	for _, conn := range []*ws.Conn{a, b} {
		id := send(conn, "room.sync", `{}`)
		event := read(conn, "world.runtime")
		var got runtimewire.Room
		if err = json.Unmarshal(event.Payload, &got); err != nil || event.RequestID != id || !reflect.DeepEqual(got, committed) {
			t.Fatalf("sync=%+v runtime=%s err=%v", event, event.Payload, err)
		}
		if event := read(conn, "npc.state"); event.RequestID != id {
			t.Fatal(event)
		}
	}
}
