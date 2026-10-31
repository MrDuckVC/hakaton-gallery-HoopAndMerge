package game

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

func TestPhotoCaptureRechecksOwnershipAndEpoch(t *testing.T) {
	s := New(time.Now)
	id := attach(t, s, "a", &testSink{})
	session := identity("a").Session
	r := photo.Request{RoomID: "room", Epoch: 1}
	if s.Capture(session) != id {
		t.Fatal("capture")
	}
	if err := s.WithCapture(context.Background(), session, id, r, func(string, func() error) error { return nil }); !errors.Is(err, worlddomain.NotReady) {
		t.Fatal(err)
	}
	s.Handle(context.Background(), "a", id, Command{Type: "world.ready", WorldVersion: "v1", MovementEpoch: 1})
	err := s.WithCapture(context.Background(), session, id, r, func(v string, check func() error) error {
		if v != "v1" {
			t.Fatal(v)
		}
		s.mu.Lock()
		s.active["a"].ended = true
		s.mu.Unlock()
		return check()
	})
	if !errors.Is(err, worlddomain.NotReady) {
		t.Fatal("late disconnect", err)
	}
	s.mu.Lock()
	s.active["a"].ended = false
	s.active["a"].ready = true
	s.active["a"].epoch = 2
	s.mu.Unlock()
	if err = s.WithCapture(context.Background(), session, id, r, func(string, func() error) error { t.Fatal("stale accepted"); return nil }); !errors.Is(err, worlddomain.NotReady) {
		t.Fatal(err)
	}
}

func TestPhotoCaptureUsesInspectGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, exhibit string
		x             float64
		wall          bool
		want          string
	}{{"boundary", "ex", 3, false, ""}, {"far", "ex", 3.000001, false, "OUT_OF_RANGE"}, {"wall", "ex", 3, true, "OUT_OF_RANGE"}, {"missing", "absent", 3, false, "NOT_FOUND"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, id, _ := setupSocial(t)
			room := worlddomain.Room{Exhibits: []worlddomain.Exhibit{{ID: "ex", Position: player.Vector{X: tc.x, Y: 1.65}}}}
			if tc.wall {
				room.Surfaces = []worlddomain.Surface{{Collidable: true, Position: player.Vector{X: 1.5, Y: 1.5}, Size: player.Vector{X: .1, Y: 3, Z: 3}}}
			}
			s.world = socialCatalog{room: room}
			s.Handle(context.Background(), "a", id, readyCommand())
			err := s.WithCapture(context.Background(), identity("a").Session, id, photo.Request{RoomID: "room", Epoch: 1, ExhibitID: tc.exhibit}, func(_ string, check func() error) error { return check() })
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || err.Error() != tc.want) {
				t.Fatal(err)
			}
		})
	}
}

func TestPhotoCaptureRejectsOldSocketAndSession(t *testing.T) {
	s := New(time.Now)
	old := attach(t, s, "a", &testSink{})
	current := attach(t, s, "a", &testSink{})
	s.Handle(context.Background(), "a", current, Command{Type: "world.ready", WorldVersion: "v1", MovementEpoch: 1})
	for _, id := range []string{old, ""} {
		if err := s.WithCapture(context.Background(), identity("a").Session, id, photo.Request{RoomID: "room", Epoch: 1}, func(string, func() error) error { return nil }); !errors.Is(err, worlddomain.NotReady) {
			t.Fatal(err)
		}
	}
	session := identity("a").Session
	session.ID = "other"
	if s.Capture(session) != "" {
		t.Fatal("other session captured socket")
	}
}
