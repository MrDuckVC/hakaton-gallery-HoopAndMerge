package game

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type lampSource struct {
	runtimeSource
	err           error
	releases      int
	before        func()
	reconcileHook func()
}

func (l *lampSource) Act(_ context.Context, _ string, actor domain.ObjectActor, a domain.ObjectAction, guard func() error, publish func(domain.Room)) error {
	if l.before != nil {
		l.before()
	}
	if err := guard(); err != nil {
		return err
	}
	if l.err != nil {
		return l.err
	}
	next, err := domain.ApplyObject(world.Room{}, l.room.Overlay, actor, a)
	if err != nil {
		return err
	}
	l.room.Overlay = next
	publish(l.room)
	return nil
}
func (l *lampSource) ReleaseLamp(_ context.Context, _ string, h domain.Holder) error {
	l.releases++
	if l.err != nil {
		return l.err
	}
	if l.room.Lamp.HolderUserID != "" && l.room.Lamp.Owner() == h {
		l.room.Revision++
		l.room.Lamp.HolderUserID = ""
		l.room.Lamp.HolderConnectionID = ""
	}
	return nil
}
func (l *lampSource) ReconcileLamp(ctx context.Context, id string, live func(string, string) bool) error {
	if l.reconcileHook != nil {
		l.reconcileHook()
	}
	if l.err != nil {
		return l.err
	}
	if l.room.Lamp.HolderUserID != "" && !live(l.room.Lamp.HolderUserID, l.room.Lamp.HolderConnectionID) {
		return l.ReleaseLamp(ctx, id, l.room.Lamp.Owner())
	}
	return nil
}
func lampGame(t *testing.T) (*Service, *lampSource, *testSink, string, *time.Time) {
	t.Helper()
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	l := &lampSource{runtimeSource: runtimeSource{room: domain.Room{Overlay: domain.Seed(world.Room{ID: "room", Version: "v1"})}}}
	s.ConfigureRuntime(l)
	s.ConfigureObjects(l)
	sink := &testSink{}
	i := identity("a")
	i.State.Position = player.Vector{X: 3, Z: 4}
	id, err := s.Reserve(i)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(id, sink); err != nil {
		t.Fatal(err)
	}
	return s, l, sink, id, &now
}
func takeCommand(rev int64) Command {
	return Command{Type: "object.act", RequestID: "take", MovementEpoch: 1, Object: domain.ObjectAction{ObjectID: "lamp_01", Action: "take", ExpectedRevision: rev}}
}
func TestLampAdmissionPublicationAndReplacement(t *testing.T) {
	s, l, sink, id, _ := lampGame(t)
	cmd := takeCommand(1)
	if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "GAME_NOT_READY" {
		t.Fatal(f)
	}
	if f := s.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	cmd.MovementEpoch = 2
	if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "STALE_MOVEMENT_EPOCH" {
		t.Fatal(f)
	}
	cmd.MovementEpoch = 1
	observer := &testSink{}
	attach(t, s, "b", observer)
	if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "" {
		t.Fatal(f)
	}
	e := sink.events[len(sink.events)-1]
	other := observer.events[len(observer.events)-1]
	if e.Type != "object.acted" || e.RequestID != "take" || e.ObjectAction != "take" || other.Type != "world.runtime" || other.RequestID != "" || e.Runtime.Revision != other.Runtime.Revision {
		t.Fatalf("events %+v %+v", e, other)
	}
	replacement := &testSink{}
	newID := attach(t, s, "a", replacement)
	if l.room.Lamp.HolderUserID != "" || replacement.events[1].Runtime.Lamp.HolderUserID != "" {
		t.Fatal("replacement retained old holder")
	}
	s.Detach("a", id)
	if !s.Current("a", newID) {
		t.Fatal("late detach removed replacement")
	}
	if f := s.Handle(t.Context(), "a", id, cmd); !f.Drop {
		t.Fatal(f)
	}
}
func TestLampReleaseFailureRetryAndLateOwner(t *testing.T) {
	s, l, sink, id, now := lampGame(t)
	if f := s.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	if f := s.Handle(t.Context(), "a", id, takeCommand(1)); f.Code != "" {
		t.Fatal(f)
	}
	l.err = auth.Unavailable
	s.Detach("a", id)
	if sink.closed != 1011 {
		t.Fatal("release failure not closed", sink.closed)
	}
	initial := l.releases
	s.retryLamps(t.Context(), *now)
	if l.releases != initial {
		t.Fatal("retry ignored delay")
	}
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 5 * time.Second} {
		*now = now.Add(delay)
		s.retryLamps(t.Context(), *now)
		if len(s.dirtyLamps) != 1 {
			t.Fatal("lost failed cleanup")
		}
	}
	l.err = nil
	*now = now.Add(5 * time.Second)
	s.retryLamps(t.Context(), *now)
	if l.room.Lamp.HolderUserID != "" || len(s.dirtyLamps) != 0 {
		t.Fatal("retry did not release")
	}
	// A dirty-room retry must preserve a new live owner.
	next := attach(t, s, "a", &testSink{})
	l.room.Lamp.HolderUserID = "a"
	l.room.Lamp.HolderConnectionID = next
	s.mu.Lock()
	s.markLampDirtyLocked("room")
	s.mu.Unlock()
	*now = now.Add(time.Second)
	s.retryLamps(t.Context(), *now)
	if l.room.Lamp.HolderConnectionID != next {
		t.Fatal("retry dropped new owner")
	}
}
func TestLampRateAndFinalAdmissionGuard(t *testing.T) {
	s, l, _, id, _ := lampGame(t)
	for i := range 11 {
		f := s.Check("a", id, Command{Type: "object.act", RequestID: strconv.Itoa(i)})
		if i < 10 && f.Code != "" || i == 10 && f.Code != "RATE_LIMITED" {
			t.Fatal(i, f)
		}
	}
	id = attach(t, s, "a", &testSink{})
	if f := s.Check("a", id, Command{Type: "object.act", RequestID: "new"}); f.Code != "RATE_LIMITED" {
		t.Fatal("reconnect reset window", f)
	}
	if f := s.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	l.before = func() { s.closeCurrent("a", id, 1011) }
	if f := s.Handle(t.Context(), "a", id, takeCommand(1)); !f.Drop {
		t.Fatal("revoked action admitted", f)
	}
	if l.room.Revision != 1 {
		t.Fatal("late command changed lamp")
	}
}
func TestLampDoorBlockedUntilRelease(t *testing.T) {
	s, sink, id, _, poses := setupGame(t)
	l := &lampSource{runtimeSource: *runtimeFixture(), err: errors.New("DB fault")}
	s.ConfigureObjects(l)
	if f := s.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	before := poses.state
	if f := s.Handle(t.Context(), "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}); f.Code != "SERVICE_UNAVAILABLE" {
		t.Fatal(f)
	}
	if poses.state != before || sink.closed != 1011 {
		t.Fatal("door acknowledged without release")
	}
}

func TestLampRetryBackoffSurvivesConcurrentInvalidation(t *testing.T) {
	s, l, _, id, now := lampGame(t)
	attach(t, s, "b", &testSink{})
	s.closeCurrent("a", id, 1011)
	l.err = auth.Unavailable
	l.reconcileHook = func() { s.InvalidateRuntime("room") }
	*now = now.Add(time.Second)
	s.retryLamps(t.Context(), *now)
	item := s.dirtyLamps["room"]
	if item.attempts != 1 || !item.due.Equal(now.Add(2*time.Second)) {
		t.Fatalf("invalidation bypassed backoff: %+v now=%v", item, *now)
	}
}
