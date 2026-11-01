package npc

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func testRoom() world.Room {
	return world.Room{ID: "room", Version: "v1", Max: player.Vector{X: 12, Y: 3, Z: 12}}
}

func TestDeterministicRoutesAndTenOrZero(t *testing.T) {
	plan, err := Prepare(testRoom())
	if err != nil {
		t.Fatal(err)
	}
	same, err := Prepare(testRoom())
	if err != nil {
		t.Fatal(err)
	}
	a, b := New(plan, 10), New(same, 10)
	if !reflect.DeepEqual(a.States(), b.States()) || len(a.States()) != 10 {
		t.Fatal("seed/count not deterministic")
	}
	now := time.Unix(100, 0)
	a.Tick(now, .2, nil)
	if reflect.DeepEqual(a.States(), b.States()) {
		t.Fatal("NPCs did not move")
	}
	a.Resize(0)
	if states := a.States(); states == nil || len(states) != 0 {
		t.Fatalf("zero must be empty: %#v", states)
	}
	a.Resize(10)
	if len(a.States()) != 10 {
		t.Fatal("count not restored")
	}
}

func TestMovementStopTalkingAndExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	plan, err := Prepare(testRoom())
	if err != nil {
		t.Fatal(err)
	}
	s := New(plan, 1)
	start := s.States()[0]
	s.Tick(now, .2, nil)
	moved := s.States()[0]
	if d := math.Hypot(moved.Position.X-start.Position.X, moved.Position.Z-start.Position.Z); math.Abs(d-.12) > 1e-9 {
		t.Fatalf("distance %v", d)
	}
	near := Visitor{UserID: "b", Position: moved.Position}
	s.Tick(now, .2, []Visitor{near})
	if n := s.States()[0]; n.State != "idle" || n.SpeedMPS != 0 || n.Position != moved.Position {
		t.Fatal(n)
	}
	s.Chat(now, near)
	s.Tick(now, .2, []Visitor{near})
	if n := s.States()[0]; n.State != "talking" || n.SpeedMPS != 0 {
		t.Fatal(n)
	}
	s.Tick(now.Add(3*time.Second-time.Nanosecond), .2, nil)
	if s.States()[0].State != "talking" {
		t.Fatal("expired early")
	}
	s.Tick(now.Add(3*time.Second), .2, nil)
	if s.States()[0].State != "walking" {
		t.Fatal("did not expire")
	}
}

func TestChatSelectionDistanceAndExtension(t *testing.T) {
	now := time.Unix(100, 0)
	plan, err := Prepare(testRoom())
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		s := New(plan, 1)
		p := s.States()[0].Position
		targets := map[string]Visitor{"a": {UserID: "a", Position: player.Vector{X: p.X + 3, Z: p.Z}}, "b": {UserID: "b", Position: player.Vector{X: p.X, Z: p.Z + 3}}}
		for _, id := range order {
			s.Chat(now, targets[id])
		}
		s.Tick(now, .2, nil)
		if n := s.States()[0]; n.State != "talking" || math.Abs(n.Yaw+math.Pi/2) > 1e-9 {
			t.Fatal("tie should face a", n)
		}
		s.Chat(now.Add(2*time.Second), targets["b"])
		s.Tick(now.Add(3*time.Second), .2, nil)
		if n := s.States()[0]; n.State != "talking" || math.Abs(math.Abs(n.Yaw)-math.Pi) > 1e-9 {
			t.Fatal("latest should face b", n)
		}
		s.Tick(now.Add(5*time.Second), .2, nil)
		if s.States()[0].State != "walking" {
			t.Fatal("extension did not expire")
		}
	}
	s := New(plan, 1)
	p := s.States()[0].Position
	s.Chat(now, Visitor{UserID: "far", Position: player.Vector{X: p.X + 3.001, Z: p.Z}})
	s.Tick(now, .2, nil)
	if s.States()[0].State != "walking" {
		t.Fatal("out of range chat triggered")
	}
}

func TestRoutesAvoidWallsAndDoorAreas(t *testing.T) {
	r := testRoom()
	r.Surfaces = []world.Surface{{Position: player.Vector{X: 6, Y: 1, Z: 6}, Size: player.Vector{X: 1, Y: 2, Z: 10}, Collidable: true}}
	r.Doors = []world.Door{{Position: player.Vector{X: 3, Z: 6}, Radius: 1}}
	plan, err := Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	s := New(plan, 10)
	now := time.Unix(100, 0)
	for range 400 {
		before := s.States()
		s.Tick(now, .2, nil)
		now = now.Add(200 * time.Millisecond)
		for i, n := range s.States() {
			if !world.Sweep(r, before[i].Position, n.Position) {
				t.Fatal("crossed wall", n)
			}
			if math.Hypot(n.Position.X-3, n.Position.Z-6) < 1.3 {
				t.Fatal("entered door", n)
			}
			if n.Phase < 0 || n.Phase >= 2*math.Pi {
				t.Fatal("invalid phase", n)
			}
		}
	}
	blocked := testRoom()
	blocked.Surfaces = []world.Surface{{Position: player.Vector{X: 6, Y: 1, Z: 6}, Size: player.Vector{X: 12, Y: 2, Z: 12}, Collidable: true}}
	if _, err := Prepare(blocked); err == nil {
		t.Fatal("accepted impassable room")
	}
}

func TestPatrolTurnConsumesFullMovementStep(t *testing.T) {
	plan := Plan{}
	plan.routes[0] = route{from: player.Vector{X: 2, Z: 2}, to: player.Vector{X: 3, Z: 2}}
	sim := New(plan, 1)
	sim.actors[0].state.Position.X = 2.96
	sim.Tick(time.Unix(100, 0), .2, nil)
	n := sim.States()[0]
	if math.Abs(n.Position.X-2.92) > 1e-9 || math.Abs(n.Phase-.12*2*math.Pi) > 1e-9 {
		t.Fatalf("turn lost movement budget: %+v", n)
	}
}

func TestIdleThresholdIsStrictAndPhaseFreezesWhileStopped(t *testing.T) {
	plan, err := Prepare(testRoom())
	if err != nil {
		t.Fatal(err)
	}
	sim := New(plan, 1)
	p := sim.States()[0].Position
	sim.Tick(time.Unix(100, 0), .2, []Visitor{{UserID: "a", Position: player.Vector{X: p.X + 1, Z: p.Z}}})
	if sim.States()[0].State != "walking" {
		t.Fatal("exactly one metre incorrectly stopped NPC")
	}
	p = sim.States()[0].Position
	phase := sim.States()[0].Phase
	sim.Tick(time.Unix(101, 0), .2, []Visitor{{UserID: "a", Position: player.Vector{X: p.X + .999, Z: p.Z}}})
	if n := sim.States()[0]; n.State != "idle" || n.Position != p || n.Phase != phase {
		t.Fatal(n)
	}
}
