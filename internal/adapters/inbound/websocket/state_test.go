package websocket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

func TestEncodeGameEvents(t *testing.T) {
	p := game.Player{User: domain.User{ID: "a", DisplayName: "Alice", AvatarColor: "#123456"}, State: player.State{RoomID: "next"}, MovementEpoch: 2, AckSeq: 7}
	for _, tc := range []struct {
		name  string
		event game.Event
		keys  []string
	}{
		{"ready", game.Event{Type: "world.ready", RequestID: "request", MovementEpoch: 2}, []string{"movement_epoch"}},
		{"entered", game.Event{Type: "world.entered", RequestID: "request", Player: p, CurrentRoom: world.RoomPackage{RoomID: "next", Assets: []world.AssetInfo{}}}, []string{"self", "current_room"}},
		{"state", game.Event{Type: "players.state", Players: []game.Player{p}}, []string{"players"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := encode(tc.event)
			if err != nil {
				t.Fatal(err)
			}
			var e map[string]any
			if err = json.Unmarshal(b, &e); err != nil {
				t.Fatal(err)
			}
			payload := e["payload"].(map[string]any)
			if len(payload) != len(tc.keys) {
				t.Fatalf("payload=%s", b)
			}
			for _, key := range tc.keys {
				if _, ok := payload[key]; !ok {
					t.Fatalf("missing %s: %s", key, b)
				}
			}
			if e["type"] != tc.event.Type || e["event_id"] == "" || e["server_time"] == "" {
				t.Fatal(e)
			}
			if tc.event.RequestID != "" && e["request_id"] != "request" {
				t.Fatal(e)
			}
			if tc.name == "state" {
				if _, ok := e["request_id"]; ok {
					t.Fatal("broadcast carries request ID", e)
				}
				got := payload["players"].([]any)[0].(map[string]any)
				if got["display_name"] != "Alice" || got["avatar_color"] != "#123456" || got["movement_epoch"] != float64(2) || got["ack_seq"] != float64(7) {
					t.Fatal(got)
				}
			}
		})
	}
	b, err := encode(game.Event{Type: "players.state"})
	if err != nil {
		t.Fatal(err)
	}
	var empty struct {
		Payload struct {
			Players []any `json:"players"`
		} `json:"payload"`
	}
	if err = json.Unmarshal(b, &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Payload.Players == nil {
		t.Fatalf("empty state must contain []: %s", b)
	}
}

type stateCatalog struct{}

func (stateCatalog) Package(id string) (world.RoomPackage, error) {
	return world.RoomPackage{RoomID: id, Assets: []world.AssetInfo{}}, nil
}
func (stateCatalog) Geometry(id string) (world.Room, error) {
	return world.Room{ID: id, Min: player.Vector{X: -10, Z: -10}, Max: player.Vector{X: 10, Y: 3, Z: 10}, Doors: []world.Door{{ID: "door", Target: "next", Position: player.Vector{}, Radius: 1.5}}}, nil
}
func (stateCatalog) Spawn(string) (player.State, error) {
	return player.State{RoomID: "next", WorldVersion: "v1", Position: player.Vector{X: 2}}, nil
}

type stateStore struct {
	mu                    sync.Mutex
	state                 player.State
	entered, continueSave chan struct{}
}

func (s *stateStore) PlayerState(context.Context, string) (player.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, nil
}
func (s *stateStore) SaveState(ctx context.Context, _ domain.Session, _, next player.State, requireSession bool) (player.State, error) {
	if requireSession && s.entered != nil {
		close(s.entered)
		select {
		case <-s.continueSave:
		case <-ctx.Done():
			return player.State{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next.UpdatedAt = s.state.UpdatedAt.Add(time.Microsecond)
	s.state = next
	return next, nil
}
func setupStateTransport(t *testing.T) (*httptest.Server, *Transport, *atomic.Int64, *stateStore) {
	t.Helper()
	clock := &atomic.Int64{}
	clock.Store(time.Unix(100, 0).UnixNano())
	service := game.New(func() time.Time { return time.Unix(0, clock.Load()) })
	store := &stateStore{state: player.State{RoomID: "r", WorldVersion: "v1"}}
	service.Configure(stateCatalog{}, store, game.NoQuestions{})
	sessions := &fakeSessions{hub: sessionevents.New(), expires: time.Now().Add(time.Hour)}
	tr := New(service, sessions, stateCatalog{}, service, []string{"http://allowed"})
	server := httptest.NewServer(tr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tr.Shutdown(ctx)
		server.Close()
	})
	return server, tr, clock, store
}
func writeStateCommand(t *testing.T, c *ws.Conn, body string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := c.Write(ctx, ws.MessageText, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

const stateReady = `{"type":"world.ready","request_id":"12345678-1234-4123-8123-123456789001","payload":{"world_version":"v1","movement_epoch":1}}`
const stateReadyAgain = `{"type":"world.ready","request_id":"12345678-1234-4123-8123-123456789002","payload":{"world_version":"v1","movement_epoch":1}}`

func TestGameTransportReadyMovementAndCompleteState(t *testing.T) {
	server, tr, clock, _ := setupStateTransport(t)
	c := dial(t, server, "a")
	if e := readEvent(t, c); e["type"] != "world.snapshot" {
		t.Fatal(e)
	}
	writeStateCommand(t, c, `{"type":"player.move","payload":{"movement_epoch":1,"seq":1,"position":{"x":0.4,"y":0,"z":0},"rotation":{"yaw":0,"pitch":0}}}`)
	if e := readEvent(t, c); e["payload"].(map[string]any)["code"] != "GAME_NOT_READY" {
		t.Fatal(e)
	}
	writeStateCommand(t, c, stateReady)
	if e := readEvent(t, c); e["type"] != "world.ready" || e["request_id"] != "12345678-1234-4123-8123-123456789001" {
		t.Fatal(e)
	}
	clock.Add(int64(100 * time.Millisecond))
	writeStateCommand(t, c, `{"type":"player.move","payload":{"movement_epoch":1,"seq":1,"position":{"x":0.4,"y":0,"z":0},"rotation":{"yaw":0.5,"pitch":0}}}`)
	writeStateCommand(t, c, stateReadyAgain)
	if e := readEvent(t, c); e["type"] != "world.ready" {
		t.Fatal(e)
	}
	tr.service.PublishState()
	e := readEvent(t, c)
	if e["type"] != "players.state" {
		t.Fatal(e)
	}
	players := e["payload"].(map[string]any)["players"].([]any)
	if len(players) != 1 {
		t.Fatal(players)
	}
	p := players[0].(map[string]any)
	if p["user_id"] != "a" || p["display_name"] != "a" || p["ack_seq"] != float64(1) || p["position"].(map[string]any)["x"] != .4 {
		t.Fatal(p)
	}
	// An impossible translation still acknowledges and applies the valid view.
	writeStateCommand(t, c, `{"type":"player.move","payload":{"movement_epoch":1,"seq":2,"position":{"x":8,"y":0,"z":0},"rotation":{"yaw":1,"pitch":0.25}}}`)
	writeStateCommand(t, c, `{"type":"world.ready","request_id":"12345678-1234-4123-8123-123456789003","payload":{"world_version":"v1","movement_epoch":1}}`)
	if e = readEvent(t, c); e["type"] != "world.ready" {
		t.Fatal(e)
	}
	tr.service.PublishState()
	e = readEvent(t, c)
	p = e["payload"].(map[string]any)["players"].([]any)[0].(map[string]any)
	if p["ack_seq"] != float64(2) || p["position"].(map[string]any)["x"] != .4 || p["rotation"].(map[string]any)["yaw"] != float64(1) {
		t.Fatal(p)
	}
}
func TestGameTransportTransitionOrdersEnteredAndSwitchesOwnership(t *testing.T) {
	server, tr, _, store := setupStateTransport(t)
	c := dial(t, server, "a")
	readEvent(t, c)
	writeStateCommand(t, c, stateReady)
	readEvent(t, c)
	store.entered = make(chan struct{})
	store.continueSave = make(chan struct{})
	writeStateCommand(t, c, `{"type":"world.enter","request_id":"12345678-1234-4123-8123-123456789004","payload":{"door_id":"door","movement_epoch":1}}`)
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("save not started")
	}
	tr.service.PublishState()
	e := readEvent(t, c)
	p := e["payload"].(map[string]any)["players"].([]any)[0].(map[string]any)
	if p["room_id"] != "r" {
		t.Fatal("uncommitted transition published", e)
	}
	if room, err := tr.service.OwningRoom(t.Context(), "a", "a"); err != nil || room != "r" {
		t.Fatal(room, err)
	}
	close(store.continueSave)
	e = readEvent(t, c)
	if e["type"] != "world.entered" || e["request_id"] != "12345678-1234-4123-8123-123456789004" {
		t.Fatal(e)
	}
	payload := e["payload"].(map[string]any)
	p = payload["self"].(map[string]any)
	if p["room_id"] != "next" || p["movement_epoch"] != float64(2) || p["ack_seq"] != float64(0) || payload["current_room"].(map[string]any)["room_id"] != "next" {
		t.Fatal(e)
	}
	if room, err := tr.service.OwningRoom(t.Context(), "a", "a"); err != nil || room != "next" {
		t.Fatal(room, err)
	}
	tr.service.PublishState()
	e = readEvent(t, c)
	p = e["payload"].(map[string]any)["players"].([]any)[0].(map[string]any)
	if p["room_id"] != "next" || p["movement_epoch"] != float64(2) {
		t.Fatal(e)
	}
	writeStateCommand(t, c, `{"type":"player.move","payload":{"movement_epoch":2,"seq":1,"position":{"x":2.1,"y":0,"z":0},"rotation":{"yaw":0,"pitch":0}}}`)
	if e = readEvent(t, c); e["payload"].(map[string]any)["code"] != "GAME_NOT_READY" {
		t.Fatal(e)
	}
}
