package game

import (
	"context"
	"errors"
	"testing"
	"time"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
)

type recoveryCatalog struct{ gameCatalog }

func (recoveryCatalog) Geometry(id string) (world.Room, error) {
	return world.Room{ID: id, Version: "v1", Max: player.Vector{X: 12, Y: 4, Z: 12}}, nil
}
func (c recoveryCatalog) Rooms() []world.Room                 { r, _ := c.Geometry("room"); return []world.Room{r} }
func (recoveryCatalog) Manifest() world.Manifest              { return world.Manifest{Version: "v1"} }
func (recoveryCatalog) SpawnPositions(string) []player.Vector { return nil }
func (recoveryCatalog) HasGuide(string) bool                  { return false }

type lostCommitStore struct {
	overlay             domain.Overlay
	exists, unavailable bool
	reads               int
}

func (s *lostCommitStore) Read(context.Context, string, string) (domain.Overlay, bool, error) {
	s.reads++
	if s.unavailable {
		return domain.Overlay{}, false, auth.Unavailable
	}
	return s.overlay.Clone(), s.exists, nil
}
func (s *lostCommitStore) CompareAndSwap(_ context.Context, _ int64, next domain.Overlay) error {
	// Database applies COMMIT; its response disappears. Caller cannot assume rollback.
	s.overlay = next.Clone()
	s.exists = true
	return domain.CommitUnknown
}
func (*lostCommitStore) IsAdmin(context.Context, string) (bool, error) { return true, nil }

type recoverySizer struct{}

func (recoverySizer) RuntimeBytes(domain.Room) (int, error) { return 1024, nil }

func TestUnknownRuntimeCommitClosesOwnersAndReconnectRereadsCommittedState(t *testing.T) {
	registry := New(time.Now)
	store := &lostCommitStore{}
	catalog := recoveryCatalog{}
	rooms := runtimecase.New(catalog, store, store, registry, recoverySizer{})
	if err := rooms.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.ConfigureRuntime(rooms)
	a, b := &testSink{}, &testSink{}
	old := attach(t, registry, "a", a)
	attach(t, registry, "b", b)
	initial, err := rooms.Read(t.Context(), "room")
	if err != nil {
		t.Fatal(err)
	}
	config := initial.Config
	config.Lighting = "cool"
	beforeA, beforeB := len(a.events), len(b.events)
	if _, err = rooms.Update(t.Context(), "room", "v1", 1, config); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	if len(a.events) != beforeA || len(b.events) != beforeB || a.closed != 1011 || b.closed != 1011 || registry.Current("a", old) {
		t.Fatal("unknown commit leaked speculative state or kept ownership")
	}
	if _, err = rooms.Geometry("room"); !errors.Is(err, auth.Unavailable) {
		t.Fatal("invalidated cache available", err)
	}
	// Reconnection must remain impossible while canonical state cannot be read.
	store.unavailable = true
	ticket, err := registry.Reserve(identity("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.AttachContext(t.Context(), ticket, &testSink{}); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	registry.Release(ticket)
	store.unavailable = false
	beforeReads := store.reads
	replacement := &testSink{}
	current := attach(t, registry, "a", replacement)
	if store.reads <= beforeReads || len(replacement.events) != 2 || replacement.events[0].Type != "world.snapshot" || replacement.events[1].Runtime.Revision != 2 || replacement.events[1].Runtime.Config.Lighting != "cool" {
		t.Fatal("reconnect used stale RAM", replacement.events)
	}
	registry.Detach("a", old)
	if !registry.Current("a", current) {
		t.Fatal("old cleanup ended new connection")
	}
}

type transitionInvalidationStore struct {
	*poseStore
	invalidate func()
}

func (s transitionInvalidationStore) SaveState(ctx context.Context, session auth.Session, old, next player.State, requireSession bool) (player.State, error) {
	if old.RoomID != next.RoomID {
		s.invalidate()
	}
	return s.poseStore.SaveState(ctx, session, old, next, requireSession)
}
func TestRuntimeInvalidationDuringDoorCommitReloadsPoseBeforeReplacement(t *testing.T) {
	registry, _, id, _, positions := setupGame(t)
	registry.ConfigureRuntime(runtimeFixture())
	registry.store = transitionInvalidationStore{positions, func() { registry.InvalidateRuntime("room") }}
	if f := registry.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	// The old room's overlay commit becomes uncertain while the player's door
	// transaction is in flight. The accepted destination is still persisted.
	registry.Handle(t.Context(), "a", id, Command{Type: "world.enter", RequestID: "enter", DoorID: "door", MovementEpoch: 1})
	if positions.state.RoomID != "next" {
		t.Fatal("door not persisted")
	}
	replacement := &testSink{}
	current := attach(t, registry, "a", replacement)
	if replacement.events[0].Snapshot.Self.State.RoomID != "next" || replacement.events[1].Runtime.RoomID != "next" {
		t.Fatal("stale pose reused before old detach", replacement.events)
	}
	registry.Detach("a", id)
	if !registry.Current("a", current) {
		t.Fatal("late detach ended replacement")
	}
}
