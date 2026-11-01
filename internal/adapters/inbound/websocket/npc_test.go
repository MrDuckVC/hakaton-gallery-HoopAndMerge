package websocket

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/npc"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	ws "github.com/coder/websocket"
)

func TestNPCStateWireAndPayloadBudget(t *testing.T) {
	for _, count := range []int{0, 10} {
		states := make([]npc.State, count)
		for i := range states {
			states[i] = npc.State{ID: "npc_0" + string(rune('0'+i)), Position: player.Vector{X: 12, Z: 12}, Yaw: -math.Pi, State: "walking", SpeedMPS: .6, Phase: 6}
		}
		data, err := encode(game.Event{Type: "npc.state", RequestID: "sync", NPCs: game.NPCSnapshot{WorldVersion: "v1", RoomID: "room", InstanceID: "instance", Seq: 1, NPCs: states}})
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type, RequestID string
			Payload         struct {
				WorldVersion string `json:"world_version"`
				RoomID       string `json:"room_id"`
				InstanceID   string `json:"room_instance_id"`
				Seq          int64  `json:"seq"`
				NPCs         []struct {
					ID    string  `json:"npc_id"`
					State string  `json:"state"`
					Speed float64 `json:"speed_mps"`
				}
			}
		}
		// request_id uses snake_case; inspect envelope separately below.
		if err = json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		if event.Payload.RoomID != "room" || event.Payload.WorldVersion != "v1" || event.Payload.InstanceID != "instance" || event.Payload.Seq != 1 || event.Payload.NPCs == nil || len(event.Payload.NPCs) != count {
			t.Fatalf("invalid NPC payload: %s", data)
		}
		if count == 10 && (event.Payload.NPCs[9].ID != "npc_09" || event.Payload.NPCs[9].Speed != .6) {
			t.Fatal(string(data))
		}
		if !strings.Contains(string(data), `"request_id":"sync"`) || len(data) > 24576 {
			t.Fatal(string(data))
		}
	}
}

func TestNPCPayloadLimitClosesConnection(t *testing.T) {
	p := newPeer(nil)
	ok := p.Enqueue(game.Event{Type: "npc.state", NPCs: game.NPCSnapshot{WorldVersion: strings.Repeat("x", 24576)}})
	if ok || !p.isClosing() {
		t.Fatal("oversized NPC payload was queued")
	}
	select {
	case code := <-p.closeCode:
		if code != 1011 {
			t.Fatal(code)
		}
	default:
		t.Fatal("missing close")
	}
}

type npcRuntime struct{}

func (npcRuntime) WithRoom(_ context.Context, id string, fn func(runtime.Room) error) error {
	return fn(runtime.Room{Overlay: runtime.Overlay{WorldVersion: "v1", RoomID: id, Revision: 1, Config: runtime.Config{NPCCount: 10}}})
}

func TestNPCRealWebSocketSnapshotSyncAndTick(t *testing.T) {
	server, tr, _ := setup(t)
	start := time.Now()
	var elapsed atomic.Int64
	registry := game.New(func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	if err := registry.ConfigureNPCs([]world.Room{{ID: "r", Version: "v1", Max: player.Vector{X: 12, Y: 3, Z: 12}}}, func() string { return "test_instance" }); err != nil {
		t.Fatal(err)
	}
	registry.ConfigureRuntime(npcRuntime{})
	tr.service = registry
	tr.handler = registry
	conn := dial(t, server, "a")
	for _, want := range []string{"world.snapshot", "world.runtime", "npc.state"} {
		if event := readEvent(t, conn); event["type"] != want {
			t.Fatal(event)
		}
	}
	request := "11111111-1111-4111-8111-111111111111"
	if err := conn.Write(t.Context(), ws.MessageText, []byte(`{"type":"room.sync","request_id":"`+request+`","payload":{}}`)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"world.runtime", "npc.state"} {
		event := readEvent(t, conn)
		if event["type"] != want || event["request_id"] != request {
			t.Fatal(event)
		}
	}
	elapsed.Store(int64(200 * time.Millisecond))
	registry.PublishNPCs()
	event := readEvent(t, conn)
	if event["type"] != "npc.state" || event["request_id"] != nil {
		t.Fatal(event)
	}
	payload := event["payload"].(map[string]any)
	if payload["seq"] != float64(3) || len(payload["npcs"].([]any)) != 10 {
		t.Fatal(payload)
	}
}
