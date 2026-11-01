package game

import (
	"math"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestMovementDeliveryJitterDoesNotRejectWalking(t *testing.T) {
	s, sink, id, now, _ := setupGame(t)
	s.Handle(t.Context(), "a", id, readyCommand())
	intervals := []time.Duration{140, 60, 100, 180, 20, 100}
	x, direction := 0.0, 1.0
	for step := range 200 {
		*now = now.Add(intervals[step%len(intervals)] * time.Millisecond)
		if x+direction*.36 > 5 || x+direction*.36 < -5 {
			direction = -direction
		}
		x += direction * .36
		s.Handle(t.Context(), "a", id, moveCommand(int64(step+1), x))
		s.PublishState()
		got := sink.events[len(sink.events)-1].Players[0]
		if math.Abs(got.State.Position.X-x) > 1e-9 || got.AckSeq != int64(step+1) {
			t.Fatalf("step %d at interval %v: got x=%v, want %v (delivery jitter rejected a valid path)", step, intervals[step%len(intervals)]*time.Millisecond, got.State.Position.X, x)
		}
	}
}

func TestMovementBudgetBoundsBurstSpeedAndRepeatedReady(t *testing.T) {
	s, sink, id, now, _ := setupGame(t)
	s.Handle(t.Context(), "a", id, readyCommand())
	*now = now.Add(time.Hour)
	s.Handle(t.Context(), "a", id, moveCommand(1, 1))
	for seq := int64(2); seq <= 20; seq++ {
		s.Handle(t.Context(), "a", id, readyCommand())
		s.Handle(t.Context(), "a", id, moveCommand(seq, 1.1))
	}
	s.PublishState()
	if got := sink.events[len(sink.events)-1].Players[0]; got.State.Position.X != 1 || got.AckSeq != 20 {
		t.Fatal("idle or repeated ready created an unbounded burst", got)
	}
}

func TestMovementBudgetCannotSustainExcessSpeed(t *testing.T) {
	s, sink, id, now, _ := setupGame(t)
	s.Handle(t.Context(), "a", id, readyCommand())
	var travelled float64
	x, direction := 0.0, 1.0
	rejected := 0
	for step := range 200 {
		*now = now.Add(100 * time.Millisecond)
		if x+direction*.5 > 5 || x+direction*.5 < -5 {
			direction = -direction
		}
		target := x + direction*.5
		s.Handle(t.Context(), "a", id, moveCommand(int64(step+1), target))
		s.PublishState()
		next := sink.events[len(sink.events)-1].Players[0].State.Position.X
		travelled += math.Abs(next - x)
		if next == x {
			rejected++
		}
		x = next
	}
	if rejected == 0 || travelled > 4*20+1e-6 {
		t.Fatalf("accepted sustained excess speed: %v metres, %d rejected", travelled, rejected)
	}
}

type movementWallCatalog struct{ gameCatalog }

func (c movementWallCatalog) Geometry(id string) (world.Room, error) {
	room, err := c.gameCatalog.Geometry(id)
	room.Surfaces = []world.Surface{{ID: "wall", Position: player.Vector{X: .5, Y: 1.5}, Size: player.Vector{X: .1, Y: 3, Z: 4}, Collidable: true}}
	return room, err
}
func TestMovementBudgetStillSweepsWallsAndOnlySpendsAcceptedDistance(t *testing.T) {
	s, sink, id, now, _ := setupGame(t)
	s.world = movementWallCatalog{}
	s.Handle(t.Context(), "a", id, readyCommand())
	*now = now.Add(time.Second)
	s.Handle(t.Context(), "a", id, moveCommand(1, 1))
	s.PublishState()
	if p := sink.events[len(sink.events)-1].Players[0]; p.State.Position.X != 0 || p.State.Rotation.Yaw != 1 {
		t.Fatal("wall crossed or rotation lost", p)
	}
	s.Handle(t.Context(), "a", id, moveCommand(2, -.5))
	s.Handle(t.Context(), "a", id, moveCommand(3, -1))
	s.Handle(t.Context(), "a", id, moveCommand(4, -1.1))
	s.PublishState()
	if p := sink.events[len(sink.events)-1].Players[0]; p.State.Position.X != -1 || p.AckSeq != 4 {
		t.Fatal("incorrect accepted-distance debit", p)
	}
}
func TestMovementBudgetResetsOnNewEpochAndConnection(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "epoch", true: "replacement"}[replace], func(t *testing.T) {
			s, _, id, now, _ := setupGame(t)
			s.Handle(t.Context(), "a", id, readyCommand())
			*now = now.Add(time.Hour)
			s.Handle(t.Context(), "a", id, moveCommand(1, 0))
			sink := &testSink{}
			epoch := int64(1)
			if replace {
				id = attach(t, s, "a", sink)
			} else {
				s.mu.Lock()
				sink = s.active["a"].sink.(*testSink)
				s.mu.Unlock()
				if f := s.Handle(t.Context(), "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}); f.Code != "" {
					t.Fatal(f)
				}
				epoch = 2
			}
			ready := readyCommand()
			ready.MovementEpoch = epoch
			s.Handle(t.Context(), "a", id, ready)
			move := moveCommand(1, .5)
			move.MovementEpoch = epoch
			s.Handle(t.Context(), "a", id, move)
			s.PublishState()
			if p := sink.events[len(sink.events)-1].Players[0]; p.State.Position.X != 0 {
				t.Fatal("old reserve survived lifecycle reset", p)
			}
		})
	}
}

func TestMovementCannotRecycleStartingCreditThroughReplacement(t *testing.T) {
	s, _, _, now, _ := setupGame(t)
	var travelled float64
	x, direction := 0.0, 1.0
	for range 20 {
		sink := &testSink{}
		id := attach(t, s, "a", sink)
		s.Handle(t.Context(), "a", id, readyCommand())
		for step := range 5 {
			*now = now.Add(100 * time.Millisecond)
			distance := .4
			if step == 0 {
				distance = .8
			}
			if x+direction*distance > 5 || x+direction*distance < -5 {
				direction = -direction
			}
			s.Handle(t.Context(), "a", id, moveCommand(int64(step+1), x+direction*distance))
			s.PublishState()
			next := sink.events[len(sink.events)-1].Players[0].State.Position.X
			travelled += math.Abs(next - x)
			x = next
		}
	}
	if travelled > 4*10+1e-6 {
		t.Fatalf("replacement minted distance: %v metres in10s", travelled)
	}
}
