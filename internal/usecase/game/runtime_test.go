package game

import (
	"context"
	"errors"
	"testing"
	"time"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

type runtimeSource struct {
	room runtime.Room
	err  error
	read func()
}

func (r *runtimeSource) WithRoom(_ context.Context, id string, fn func(runtime.Room) error) error {
	if r.read != nil {
		r.read()
	}
	if r.err != nil {
		return r.err
	}
	room := r.room
	room.RoomID = id
	return fn(room)
}
func runtimeFixture() *runtimeSource {
	return &runtimeSource{room: runtime.Room{Overlay: runtime.Overlay{WorldVersion: "v1", RoomID: "room", Revision: 1}}}
}

func TestRuntimeSnapshotSyncBeforeReadyAndReconnectLimits(t *testing.T) {
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	source := runtimeFixture()
	s.ConfigureRuntime(source)
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	if len(sink.events) != 2 || sink.events[0].Type != "world.snapshot" || sink.events[1].Type != "world.runtime" {
		t.Fatalf("order: %+v", sink.events)
	}
	for _, request := range []string{"one", "two"} {
		cmd := Command{Type: "room.sync", RequestID: request}
		if f := s.Check("a", id, cmd); f.Code != "" {
			t.Fatal(f)
		}
		if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "" {
			t.Fatal(f)
		}
		last := sink.events[len(sink.events)-1]
		if last.Type != "world.runtime" || last.RequestID != request || last.Runtime.Revision != 1 {
			t.Fatal(last)
		}
	}
	id = attach(t, s, "a", &testSink{})
	if f := s.Check("a", id, Command{Type: "room.sync", RequestID: "three"}); f.Code != "RATE_LIMITED" || f.RetryAfterMS != 10000 {
		t.Fatal(f)
	}
	now = now.Add(10 * time.Second)
	if f := s.Check("a", id, Command{Type: "room.sync", RequestID: "four"}); f.Code != "" {
		t.Fatal(f)
	}
	if f := s.Handle(t.Context(), "a", "stale", Command{Type: "room.sync"}); !f.Drop {
		t.Fatal(f)
	}
}
func TestRuntimeReadFailureDoesNotAttachOrEmit(t *testing.T) {
	s := New(time.Now)
	source := runtimeFixture()
	source.err = auth.Unavailable
	s.ConfigureRuntime(source)
	ticket, err := s.Reserve(identity("a"))
	if err != nil {
		t.Fatal(err)
	}
	sink := &testSink{}
	if err = s.AttachContext(t.Context(), ticket, sink); !errors.Is(err, auth.Unavailable) {
		t.Fatal(err)
	}
	if len(sink.events) != 0 || s.Current("a", ticket) {
		t.Fatal("failed runtime attached")
	}
}
func TestRuntimePublicationRoomScopeAndInvalidation(t *testing.T) {
	s := New(time.Now)
	s.ConfigureRuntime(runtimeFixture())
	a, b, c := &testSink{}, &testSink{}, &testSink{}
	aID := attach(t, s, "a", a)
	attach(t, s, "b", b)
	other := identity("c")
	other.State.RoomID = "other"
	other.Package.RoomID = "other"
	cID, err := s.Reserve(other)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(cID, c); err != nil {
		t.Fatal(err)
	}
	before := len(c.events)
	room := runtimeFixture().room
	room.Revision = 2
	s.PublishRuntime(room)
	for _, sink := range []*testSink{a, b} {
		last := sink.events[len(sink.events)-1]
		if last.Type != "world.runtime" || last.Runtime.Revision != 2 || last.RequestID != "" {
			t.Fatal(last)
		}
	}
	if len(c.events) != before {
		t.Fatal("cross-room publication")
	}
	s.InvalidateRuntime("room")
	if a.closed != 1011 || b.closed != 1011 || c.closed != 0 || s.Current("a", aID) {
		t.Fatal("invalidation failed")
	}
}
func TestRuntimeEnterOrderAndDurableReadOutsideMutex(t *testing.T) {
	s, sink, id, _, _ := setupGame(t)
	source := runtimeFixture()
	// Querying registry from a durable read deadlocks if game mutex covers I/O.
	source.read = func() { s.Current("a", id) }
	s.ConfigureRuntime(source)
	if f := s.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	if f := s.Handle(t.Context(), "a", id, Command{Type: "world.enter", RequestID: "enter", DoorID: "door", MovementEpoch: 1}); f.Code != "" {
		t.Fatal(f)
	}
	n := len(sink.events)
	if sink.events[n-2].Type != "world.entered" || sink.events[n-1].Type != "world.runtime" || sink.events[n-1].Runtime.RoomID != "next" {
		t.Fatal(sink.events)
	}
}

type roomSnapshots struct{}

func (roomSnapshots) EnqueueSnapshot(_ string, request string, sink Sink) bool {
	return sink.Enqueue(Event{Type: "npc.state", RequestID: request})
}
func TestRuntimeAlwaysPrecedesRoomSimulationSnapshots(t *testing.T) {
	s := New(time.Now)
	s.ConfigureRuntime(runtimeFixture())
	s.ConfigureRoomSnapshots(roomSnapshots{})
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	if len(sink.events) != 3 || sink.events[0].Type != "world.snapshot" || sink.events[1].Type != "world.runtime" || sink.events[2].Type != "npc.state" {
		t.Fatal(sink.events)
	}
	if f := s.Handle(t.Context(), "a", id, Command{Type: "room.sync", RequestID: "sync"}); f.Code != "" {
		t.Fatal(f)
	}
	n := len(sink.events)
	if sink.events[n-2].Type != "world.runtime" || sink.events[n-1].Type != "npc.state" || sink.events[n-2].RequestID != "sync" || sink.events[n-1].RequestID != "sync" {
		t.Fatal(sink.events)
	}
}
