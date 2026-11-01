package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type catalog struct{ room world.Room }

func (c catalog) Geometry(id string) (world.Room, error) {
	if id != c.room.ID {
		return world.Room{}, world.NotFound
	}
	return c.room, nil
}
func (c catalog) Manifest() world.Manifest              { return world.Manifest{Version: c.room.Version} }
func (c catalog) Rooms() []world.Room                   { return []world.Room{c.room} }
func (c catalog) SpawnPositions(string) []player.Vector { return []player.Vector{{X: 2, Z: 2}} }
func (c catalog) HasGuide(id string) bool               { return id == "g" }
func (c catalog) Package(id string) (world.RoomPackage, error) {
	return world.RoomPackage{RoomID: id}, nil
}
func (c catalog) Spawn(string) (player.State, error) { return player.State{}, nil }

type store struct {
	mu     sync.Mutex
	o      domain.Overlay
	exists bool
	err    error
}

func (s *store) Read(context.Context, string, string) (domain.Overlay, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.o.Clone(), s.exists, nil
}
func (s *store) CompareAndSwap(_ context.Context, expected int64, next domain.Overlay) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	rev := int64(1)
	if s.exists {
		rev = s.o.Revision
	}
	if rev != expected {
		return domain.RevisionConflict
	}
	s.o = next.Clone()
	s.exists = true
	return nil
}

type roles struct {
	admin bool
	err   error
}

func (r roles) IsAdmin(context.Context, string) (bool, error) { return r.admin, r.err }

type publisher struct {
	rooms   []domain.Room
	invalid []string
}

func (p *publisher) PublishRuntime(r domain.Room) { p.rooms = append(p.rooms, r) }
func (p *publisher) InvalidateRuntime(id string)  { p.invalid = append(p.invalid, id) }

type sizer struct{ n int }

func (s sizer) RuntimeBytes(r domain.Room) (int, error) {
	if r.Revision == 1 {
		return 1000, nil
	}
	return s.n, nil
}
func fixture(t *testing.T) (*Service, *store, *publisher) {
	t.Helper()
	r := world.Room{ID: "r", Version: "v", Title: "Room", Max: player.Vector{X: 12, Y: 4, Z: 12}, Guides: []world.Guide{{ID: "g", Position: player.Vector{X: 6, Z: 7.5}, Radius: 3}}, Doors: []world.Door{{ID: "d", Position: player.Vector{X: 1, Z: 6}}}}
	st := &store{}
	pub := &publisher{}
	s := New(catalog{r}, st, roles{admin: true}, pub, sizer{1000})
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, st, pub
}
func TestConcurrentCASPublishesOnlyDurableWinner(t *testing.T) {
	s, st, p := fixture(t)
	r, err := s.Read(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, e := s.Update(context.Background(), "r", "v", 1, r.Config); results <- e }()
	}
	a, b := <-results, <-results
	valid := a == nil && errors.Is(b, domain.RevisionConflict) || b == nil && errors.Is(a, domain.RevisionConflict)
	if !valid {
		t.Fatalf("results %v, %v", a, b)
	}
	if len(p.rooms) != 1 || st.o.Revision != 2 {
		t.Fatalf("published %d rev %d", len(p.rooms), st.o.Revision)
	}
}
func TestSQLFailureAndUnknownCommitNeverPublish(t *testing.T) {
	for _, failure := range []error{errors.New("SQL unavailable"), domain.CommitUnknown} {
		t.Run(failure.Error(), func(t *testing.T) {
			s, st, p := fixture(t)
			r, _ := s.Read(context.Background(), "r")
			st.err = failure
			_, err := s.Update(context.Background(), "r", "v", 1, r.Config)
			if !errors.Is(err, auth.Unavailable) || len(p.rooms) != 0 {
				t.Fatalf("error %v publications %d", err, len(p.rooms))
			}
			if errors.Is(failure, domain.CommitUnknown) && len(p.invalid) != 1 {
				t.Fatal("uncertain commit did not invalidate connections")
			}
		})
	}
}
func TestStoreBusyRaceIsConflictWithoutPublication(t *testing.T) {
	s, st, p := fixture(t)
	r, _ := s.Read(context.Background(), "r")
	st.err = domain.ObjectBusy
	_, err := s.Update(context.Background(), "r", "v", 1, r.Config)
	if !errors.Is(err, domain.ObjectBusy) || len(p.rooms) != 0 {
		t.Fatalf("got %v, publications %d", err, len(p.rooms))
	}
}
func TestEffectivePlacementAndRestart(t *testing.T) {
	s, st, p := fixture(t)
	r, _ := s.Read(context.Background(), "r")
	r.Config.GuidePlacements[0].Position = player.Vector{X: 8, Z: 8}
	got, err := s.Update(context.Background(), "r", "v", 1, r.Config)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.Geometry("r")
	if g.Guides[0].Position != got.Guides[0].Position || p.rooms[0].Guides[0].Position != g.Guides[0].Position {
		t.Fatal("effective geometry differs from published state")
	}
	s2 := New(s.catalog, st, roles{}, p, sizer{1000})
	if err = s2.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	g2, _ := s2.Geometry("r")
	if g2.Guides[0].Position != g.Guides[0].Position {
		t.Fatal("restart lost durable placement")
	}
	g2.Guides[0].Position.X = 0
	g3, _ := s2.Geometry("r")
	if g3.Guides[0].Position.X == 0 {
		t.Fatal("caller mutated effective state")
	}
}
func TestUpdateNegativeCases(t *testing.T) {
	s, st, p := fixture(t)
	r, _ := s.Read(context.Background(), "r")
	if _, e := s.Update(context.Background(), "r", "old", 1, r.Config); !errors.Is(e, world.VersionMismatch) {
		t.Fatal(e)
	}
	if _, e := s.Update(context.Background(), "missing", "v", 1, r.Config); !errors.Is(e, world.NotFound) {
		t.Fatal(e)
	}
	s.sizer = sizer{32769}
	if _, e := s.Update(context.Background(), "r", "v", 1, r.Config); !errors.Is(e, auth.Validation) {
		t.Fatal(e)
	}
	s.sizer = sizer{1000}
	st.o = r.Overlay
	st.o.Lamp.HolderUserID = "u"
	st.o.Lamp.HolderConnectionID = "c"
	st.exists = true
	if _, e := s.Update(context.Background(), "r", "v", 1, r.Config); !errors.Is(e, domain.ObjectBusy) {
		t.Fatal(e)
	}
	if len(p.rooms) != 0 {
		t.Fatal("rejected write published")
	}
	s.roles = roles{}
	if e := s.RequireAdmin(context.Background(), "u"); !errors.Is(e, domain.Forbidden) {
		t.Fatal(e)
	}
}
func TestForeignOverlayRejectedAtStartup(t *testing.T) {
	s, st, _ := fixture(t)
	r, _ := s.Read(context.Background(), "r")
	st.o = r.Overlay
	st.o.WorldVersion = "old"
	st.exists = true
	if err := s.Initialize(context.Background()); err == nil {
		t.Fatal("foreign overlay applied")
	}
}
