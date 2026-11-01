package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type profileCatalog struct {
	catalog
	extra world.Room
}

func (c profileCatalog) Rooms() []world.Room { return []world.Room{c.room, c.extra} }
func (c profileCatalog) Geometry(id string) (world.Room, error) {
	if id == c.extra.ID {
		return c.extra, nil
	}
	return c.catalog.Geometry(id)
}
func (c profileCatalog) Profile(string) (guide.Profile, error) {
	return guide.Profile{Personality: "original", Style: "style", Topic: "topic", Constraints: []string{"constraint"}, Materials: []guide.Material{{ID: "own", Text: "данные", Sources: []string{"local"}}}}, nil
}

type profileStore struct {
	mu      sync.Mutex
	rooms   map[string]domain.Overlay
	profile guide.Override
	fail    error
}

func (s *profileStore) Read(_ context.Context, _, id string) (domain.Overlay, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.rooms[id]
	return o.Clone(), ok, nil
}
func (*profileStore) CompareAndSwap(context.Context, int64, domain.Overlay) error {
	return auth.Unavailable
}
func (s *profileStore) ReadProfile(context.Context, string, string) (guide.Override, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.profile
	o.Settings = o.Settings.Clone()
	return o, o.Revision != 0, nil
}
func (s *profileStore) UpdateProfile(_ context.Context, expected int64, next guide.Override, rooms []domain.Overlay) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	rev := max(int64(1), s.profile.Revision)
	if expected != rev {
		return domain.RevisionConflict
	}
	for _, r := range rooms {
		if max(int64(1), s.rooms[r.RoomID].Revision) != r.Revision-1 {
			return domain.RevisionConflict
		}
	}
	s.profile = next
	for _, r := range rooms {
		s.rooms[r.RoomID] = r.Clone()
	}
	return nil
}
func profilesFixture(t *testing.T) (*Service, *profileStore, *publisher) {
	t.Helper()
	old, _, _ := fixture(t)
	r := old.catalog.Rooms()[0]
	r.Guides[0].DisplayName = "Original"
	r.Guides[0].Subtitle = "Subtitle"
	other := r
	other.ID = "r2"
	c := profileCatalog{catalog: catalog{r}, extra: other}
	st := &profileStore{rooms: make(map[string]domain.Overlay)}
	pub := &publisher{}
	s := New(c, st, roles{admin: true}, pub, sizer{1000})
	s.ConfigureProfiles(c, st)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, st, pub
}
func TestProfileCASChangesEveryRoomAndPreservesInstructionsOnlyUpdate(t *testing.T) {
	s, _, p := profilesFixture(t)
	original, err := s.Guide(t.Context(), "g")
	if err != nil || original.Override.Revision != 1 || len(original.Override.Settings.MaterialIDs) != 1 {
		t.Fatal(original, err)
	}
	settings := original.Override.Settings
	settings.DisplayName = "New name"
	results := make(chan error, 2)
	for range 2 {
		go func() { _, e := s.UpdateGuide(t.Context(), "g", "v", 1, settings); results <- e }()
	}
	a, b := <-results, <-results
	valid := a == nil && errors.Is(b, domain.RevisionConflict) || b == nil && errors.Is(a, domain.RevisionConflict)
	if !valid {
		t.Fatal(a, b)
	}
	if len(p.rooms) != 2 {
		t.Fatal("broadcast count", len(p.rooms))
	}
	for _, id := range []string{"r", "r2"} {
		r, e := s.Read(t.Context(), id)
		if e != nil || r.Revision != 2 || r.Guides[0].DisplayName != "New name" {
			t.Fatal(r, e)
		}
		geometry, e := s.Geometry(id)
		if e != nil || geometry.Guides[0].DisplayName != "New name" {
			t.Fatal(geometry, e)
		}
	}
	settings.Personality = "new instructions"
	if _, err = s.UpdateGuide(t.Context(), "g", "v", 2, settings); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Read(t.Context(), "r")
	if r.Revision != 2 || len(p.rooms) != 2 {
		t.Fatal("instructions changed room revision")
	}
}
func TestProfileFailureAndValidationNeverPublish(t *testing.T) {
	for _, failure := range []error{auth.Unavailable, domain.CommitUnknown} {
		s, st, p := profilesFixture(t)
		g, _ := s.Guide(t.Context(), "g")
		settings := g.Override.Settings
		settings.Subtitle = "new subtitle"
		st.fail = failure
		if _, err := s.UpdateGuide(t.Context(), "g", "v", 1, settings); !errors.Is(err, auth.Unavailable) {
			t.Fatal(err)
		}
		if len(p.rooms) != 0 {
			t.Fatal("published uncommitted profile")
		}
		if errors.Is(failure, domain.CommitUnknown) && len(p.invalid) != 2 {
			t.Fatal("did not invalidate all rooms")
		}
	}
	s, _, _ := profilesFixture(t)
	g, _ := s.Guide(t.Context(), "g")
	if _, err := s.UpdateGuide(t.Context(), "g", "wrong", 1, g.Override.Settings); !errors.Is(err, world.VersionMismatch) {
		t.Fatal(err)
	}
	g.Override.Settings.MaterialIDs = []string{"foreign"}
	if _, err := s.UpdateGuide(t.Context(), "g", "v", 1, g.Override.Settings); !errors.Is(err, auth.Validation) {
		t.Fatal(err)
	}
	if _, err := s.Guide(t.Context(), "missing"); !errors.Is(err, world.NotFound) {
		t.Fatal(err)
	}
}
func TestProfileSeedRejectsInconsistentIdentity(t *testing.T) {
	s, _, _ := profilesFixture(t)
	c := s.catalog.(profileCatalog)
	c.extra.Guides = append([]world.Guide(nil), c.extra.Guides...)
	c.extra.Guides[0].Subtitle = "different"
	s.catalog = c
	if err := s.Initialize(t.Context()); err == nil {
		t.Fatal("ambiguous guide seed accepted")
	}
}
