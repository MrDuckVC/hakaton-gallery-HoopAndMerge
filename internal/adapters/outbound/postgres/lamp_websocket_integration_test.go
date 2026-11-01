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

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/runtimewire"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	authcase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
	ws "github.com/coder/websocket"
)

func TestLampTwoClientsReconnectAndSQLFaultIntegration(t *testing.T) {
	ctx, dsn, pool := testDatabase(t)
	if _, err := Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	catalog := runtimeCatalog{}
	crypto := security.Crypto{}
	accounts := authcase.New(NewAuthStore(pool), crypto, catalog, sessionevents.New(), time.Now)
	registry := game.New(time.Now)
	store := NewRuntimeStore(pool)
	rooms := runtimecase.New(catalog, store, store, registry, runtimewire.Sizer{})
	registry.ConfigureObjects(rooms)
	if err := rooms.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	registry.ConfigureRuntime(rooms)
	registry.Configure(rooms, NewGameStore(pool), game.NoQuestions{})
	transport := wsapi.New(registry, accounts, catalog, registry, []string{"http://allowed"})
	server := httpapi.NewServer("")
	server.ConfigureWebSocket(transport)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	cleanupCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); registry.RunLampCleanup(cleanupCtx) }()
	defer func() {
		stop()
		<-done
		bounded, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = transport.Shutdown(bounded)
	}()
	read := func(conn *ws.Conn) runtimeTestEvent {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, body, err := conn.Read(bounded)
		if err != nil {
			t.Fatal(err)
		}
		var e runtimeTestEvent
		if err = json.Unmarshal(body, &e); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "holder_connection_id") {
			t.Fatal("private owner leaked")
		}
		return e
	}
	send := func(conn *ws.Conn, id, kind, payload string) {
		t.Helper()
		if err := conn.Write(ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":%q,"request_id":%q,"payload":%s}`, kind, id, payload))); err != nil {
			t.Fatal(err)
		}
	}
	connect := func(token string) (*ws.Conn, runtimewire.Room) {
		t.Helper()
		conn, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		if e := read(conn); e.Type != "world.snapshot" {
			t.Fatal(e)
		}
		e := read(conn)
		var r runtimewire.Room
		if e.Type != "world.runtime" {
			t.Fatal(e)
		}
		if err = json.Unmarshal(e.Payload, &r); err != nil {
			t.Fatal(err)
		}
		send(conn, crypto.NewID(), "world.ready", `{"world_version":"v1","movement_epoch":1}`)
		if e = read(conn); e.Type != "world.ready" {
			t.Fatal(e)
		}
		return conn, r
	}
	alice, err := accounts.Register(ctx, "lamp_alice", "Alice", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := accounts.Register(ctx, "lamp_bob", "Bob", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := connect(alice.Token)
	b, _ := connect(bob.Token)
	if e := read(a); e.Type != "player.joined" {
		t.Fatal(e)
	}
	ids := []string{crypto.NewID(), crypto.NewID()}
	clients := []*ws.Conn{a, b}
	tokens := []string{alice.Token, bob.Token}
	payload := `{"object_id":"lamp_01","action":"take","movement_epoch":1,"expected_revision":1}`
	// Both commands are outstanding before reading either response.
	for i, c := range clients {
		send(c, ids[i], "object.act", payload)
	}
	winner := -1
	for i, c := range clients {
		for {
			e := read(c)
			if e.Type == "world.runtime" {
				continue
			}
			if e.RequestID != ids[i] {
				t.Fatal(e)
			}
			if e.Type == "object.acted" {
				if winner != -1 {
					t.Fatal("two holders")
				}
				winner = i
			} else if e.Type != "error" || !strings.Contains(string(e.Payload), "OBJECT_BUSY") {
				t.Fatal(e)
			}
			break
		}
	}
	if winner < 0 {
		t.Fatal("no holder")
	}
	durable, _, err := store.Read(ctx, "v1", "safe")
	if err != nil || durable.Revision != 2 || durable.Lamp.HolderUserID == "" {
		t.Fatal(durable, err)
	}
	w, other := clients[winner], clients[1-winner]
	send(w, ids[winner], "object.act", payload)
	if e := read(w); e.Type != "error" || !strings.Contains(string(e.Payload), "DUPLICATE_REQUEST") {
		t.Fatal(e)
	}
	send(w, crypto.NewID(), "object.act", `{"object_id":"lamp_01","action":"aim","movement_epoch":1,"expected_revision":2,"yaw":0,"head_pitch":0}`)
	if e := read(w); e.Type != "error" || !strings.Contains(string(e.Payload), "OBJECT_BUSY") {
		t.Fatal(e)
	}
	for _, step := range []struct {
		action   string
		revision int64
		extra    string
	}{{"place", 2, `,"slot_id":"a"`}, {"power", 3, `,"on":true`}, {"take", 4, ""}} {
		id := crypto.NewID()
		send(w, id, "object.act", fmt.Sprintf(`{"object_id":"lamp_01","action":%q,"movement_epoch":1,"expected_revision":%d%s}`, step.action, step.revision, step.extra))
		e := read(w)
		if e.Type != "object.acted" || e.RequestID != id {
			t.Fatal(e)
		}
		if e = read(other); e.Type != "world.runtime" || e.RequestID != "" {
			t.Fatal(e)
		}
	}
	// A real SQL constraint rejects cleanup until the test removes the fault.
	if _, err = pool.Exec(ctx, `ALTER TABLE room_overlays ADD CONSTRAINT lamp_release_fault CHECK(revision < 6)`); err != nil {
		t.Fatal(err)
	}
	_ = w.CloseNow()
	if e := read(other); e.Type != "player.left" {
		t.Fatal(e)
	}
	held, _, err := store.Read(ctx, "v1", "safe")
	if err != nil || held.Revision != 5 || held.Lamp.HolderUserID == "" {
		t.Fatal("failed release changed durable state", held, err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE room_overlays DROP CONSTRAINT lamp_release_fault`); err != nil {
		t.Fatal(err)
	}
	if e := read(other); e.Type != "world.runtime" {
		t.Fatal(e)
	}
	restored, _, err := store.Read(ctx, "v1", "safe")
	if err != nil || restored.Revision != 6 || restored.Lamp.HolderUserID != "" {
		t.Fatal("retry failed", restored, err)
	}
	_, snapshot := connect(tokens[winner])
	if snapshot.Revision != 6 || snapshot.Lamp.HolderUserID != nil {
		t.Fatal("reconnect disagrees", snapshot)
	}
}
