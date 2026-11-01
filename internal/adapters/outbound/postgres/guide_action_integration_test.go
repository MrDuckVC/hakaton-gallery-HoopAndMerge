package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	authcase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	guidecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
	ws "github.com/coder/websocket"
)

type actionModel struct{ calls atomic.Int64 }

func (m *actionModel) Stream(_ context.Context, _ guidecase.ModelRequest, emit func(string) error) error {
	m.calls.Add(1)
	return emit("Это учебное объяснение цвета и света.")
}

func TestGuideActionWebSocketDurabilityAndConversationIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	catalog := profileRuntimeCatalog{}
	crypto := security.Crypto{}
	accounts := authcase.New(NewAuthStore(pool), crypto, catalog, sessionevents.New(), time.Now)
	var offset atomic.Int64
	registry := game.New(func() time.Time { return time.Now().Add(time.Duration(offset.Load())) })
	store := NewRuntimeStore(pool)
	rooms := runtimecase.New(catalog, store, store, registry, runtimewire.Sizer{})
	if err := rooms.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	registry.ConfigureRuntime(rooms)
	registry.ConfigureObjects(rooms)
	registry.ConfigureGuideActions(rooms)
	if err := registry.ConfigureNPCs(catalog.Rooms(), crypto.NewID); err != nil {
		t.Fatal(err)
	}
	guideStore := NewGuideStore(pool)
	model := &actionModel{}
	questions := guidecase.New(guideStore, rooms, accounts, registry, model, guidecase.Clock{Now: time.Now, After: time.After}, crypto.NewID)
	questions.ConfigureContext(catalog, guideStore)
	registry.Configure(rooms, NewGameStore(pool), questions)
	registry.ConfigureGuides(questions)
	config := domain.Seed(catalog.Rooms()[0]).Config
	config.GuidePlacements[0].Position = player.Vector{X: 2, Z: 4.5}
	initial, err := rooms.Update(ctx, "safe", "v1", 1, config)
	if err != nil {
		t.Fatal(err)
	}
	transport := wsapi.New(registry, accounts, catalog, registry, []string{"http://allowed"})
	server := httpapi.NewServer("")
	server.ConfigureWebSocket(transport)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	workerCtx, stopWorker := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); questions.Run(workerCtx) }()
	defer func() {
		stopWorker()
		<-done
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = transport.Shutdown(shutdown)
	}()
	read := func(conn *ws.Conn, want string) runtimeTestEvent {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		for {
			_, body, e := conn.Read(bounded)
			if e != nil {
				t.Fatal(e)
			}
			var event runtimeTestEvent
			if e = json.Unmarshal(body, &event); e != nil {
				t.Fatal(e)
			}
			if event.Type == "player.joined" || event.Type == "player.left" {
				continue
			}
			if event.Type != want {
				t.Fatalf("want %s got %s", want, body)
			}
			return event
		}
	}
	decodeRoom := func(e runtimeTestEvent) runtimewire.Room {
		t.Helper()
		var r runtimewire.Room
		if err = json.Unmarshal(e.Payload, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	connect := func(token string, want runtimewire.Room) *ws.Conn {
		t.Helper()
		c, _, e := ws.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + token}}})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = c.CloseNow() })
		read(c, "world.snapshot")
		if got := decodeRoom(read(c, "world.runtime")); !reflect.DeepEqual(got, want) {
			t.Fatalf("reconnect runtime %+v want %+v", got, want)
		}
		read(c, "npc.state")
		return c
	}
	send := func(c *ws.Conn, kind, id string, payload any) {
		t.Helper()
		body, e := json.Marshal(map[string]any{"type": kind, "request_id": id, "payload": payload})
		if e != nil {
			t.Fatal(e)
		}
		if e = c.Write(ctx, ws.MessageText, body); e != nil {
			t.Fatal(e)
		}
	}
	ready := func(c *ws.Conn) {
		send(c, "world.ready", crypto.NewID(), map[string]any{"world_version": "v1", "movement_epoch": 1})
		read(c, "world.ready")
	}
	alice, err := accounts.Register(ctx, "action_alice", "Alice", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := accounts.Register(ctx, "action_bob", "Bob", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	committed := runtimewire.Public(initial)
	a, b := connect(alice.Token, committed), connect(bob.Token, committed)
	action := func(text string, epoch, rev int64) string {
		t.Helper()
		offset.Add(int64(11 * time.Second))
		id := crypto.NewID()
		send(a, "guide.action", id, map[string]any{"guide_id": "guide", "text": text, "movement_epoch": epoch, "expected_revision": rev})
		return id
	}
	failure := func(id, code string) {
		t.Helper()
		e := read(a, "error")
		if e.RequestID != id || !strings.Contains(string(e.Payload), `"code":"`+code+`"`) {
			t.Fatal(e)
		}
	}
	failure(action("покажи макет", 1, 2), "GAME_NOT_READY")
	ready(a)
	failure(action("покажи макет", 2, 2), "STALE_MOVEMENT_EPOCH")
	failure(action("создай портал", 1, 2), "UNSUPPORTED_ACTION")
	failure(action("покажи макет", 1, 1), "REVISION_CONFLICT")
	for _, step := range []struct {
		text   string
		effect domain.GuideEffect
	}{
		{" ПОКАЖИ\tМАКЕТ!!! ", domain.ShowInstallation}, {"добавь макет", domain.ShowInstallation}, {"тёплый свет", domain.WarmLight}, {"холодный свет", domain.CoolLight}, {"скрой макет", domain.HideInstallation},
	} {
		id := action(step.text, 1, committed.Revision)
		e := read(a, "guide.action.completed")
		var completion struct {
			GuideID string             `json:"guide_id"`
			Effect  domain.GuideEffect `json:"effect"`
			Runtime runtimewire.Room   `json:"runtime"`
		}
		if err = json.Unmarshal(e.Payload, &completion); err != nil || e.RequestID != id || completion.GuideID != "guide" || completion.Effect != step.effect || completion.Runtime.Revision != committed.Revision+1 {
			t.Fatal(e, err)
		}
		durable, _, e2 := store.Read(ctx, "v1", "safe")
		if e2 != nil || durable.Revision != completion.Runtime.Revision || durable.Config.InstallationVisible != completion.Runtime.Config.InstallationVisible || durable.Config.Lighting != completion.Runtime.Config.Lighting {
			t.Fatal("completion before durable mutation", durable, e2)
		}
		committed = completion.Runtime
		read(a, "npc.state")
		broadcast := read(b, "world.runtime")
		if broadcast.RequestID != "" || !reflect.DeepEqual(decodeRoom(broadcast), committed) {
			t.Fatal(broadcast)
		}
		read(b, "npc.state")
		send(a, "guide.action", id, map[string]any{"guide_id": "guide", "text": step.text, "movement_epoch": 1, "expected_revision": committed.Revision - 1})
		failure(id, "DUPLICATE_REQUEST")
	}
	assertNoConversation := func() {
		t.Helper()
		for _, table := range []string{"guide_generations", "memory_events", "guide_memories"} {
			var count int
			if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
				t.Fatal(table, count, err)
			}
		}
		if model.calls.Load() != 0 {
			t.Fatal("actions invoked model")
		}
	}
	assertNoConversation()
	// A normal question with an action phrase remains an educational conversation.
	askID := crypto.NewID()
	send(a, "guide.ask", askID, map[string]any{"guide_id": "guide", "text": "покажи макет"})
	for _, kind := range []string{"guide.accepted", "guide.started", "guide.delta", "guide.completed"} {
		if e := read(a, kind); e.RequestID != askID {
			t.Fatal(e)
		}
	}
	if model.calls.Load() != 1 {
		t.Fatal("question did not use model")
	}
	var memories, generations, events int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM guide_generations),(SELECT count(*) FROM guide_memories),(SELECT count(*) FROM memory_events)`).Scan(&generations, &memories, &events); err != nil || generations != 1 || memories != 1 {
		t.Fatal(generations, memories, events, err)
	}
	// SQL rejection must not acknowledge or broadcast a proposed mutation.
	if _, err = pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE room_overlays ADD CONSTRAINT reject_action CHECK(revision <= %d)`, committed.Revision)); err != nil {
		t.Fatal(err)
	}
	failure(action("тёплый свет", 1, committed.Revision), "SERVICE_UNAVAILABLE")
	for _, c := range []*ws.Conn{a, b} {
		id := crypto.NewID()
		send(c, "room.sync", id, map[string]any{})
		e := read(c, "world.runtime")
		if e.RequestID != id || !reflect.DeepEqual(decodeRoom(e), committed) {
			t.Fatal(e)
		}
		read(c, "npc.state")
	}
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM guide_generations),(SELECT count(*) FROM guide_memories),(SELECT count(*) FROM memory_events)`).Scan(&generations, &memories, &events); err != nil || generations != 1 || memories != 1 || events != 0 || model.calls.Load() != 1 {
		t.Fatal("action affected conversation", generations, memories, events, err)
	}
	// Replacement reconnect and a new runtime service both reread the durable overlay.
	a = connect(alice.Token, committed)
	restarted := runtimecase.New(catalog, NewRuntimeStore(pool), store, registry, runtimewire.Sizer{})
	if err = restarted.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Read(ctx, "safe")
	if err != nil || !reflect.DeepEqual(runtimewire.Public(restored), committed) {
		t.Fatal(restored, err)
	}
}
