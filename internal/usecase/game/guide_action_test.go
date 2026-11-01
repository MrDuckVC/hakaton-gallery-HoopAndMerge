package game

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type actionSource struct {
	runtimeSource
	before func()
	err    error
	calls  int
}

func (a *actionSource) Action(_ context.Context, _ string, p player.Vector, cmd domain.GuideAction, guard func() error, publish func(domain.Room, domain.GuideEffect)) error {
	a.calls++
	if a.before != nil {
		a.before()
	}
	if err := guard(); err != nil {
		return err
	}
	if a.err != nil {
		return a.err
	}
	next, effect, err := domain.ApplyGuideAction(world.Room{Guides: []world.Guide{{ID: "g"}}}, a.room.Overlay, p, cmd)
	if err != nil {
		return err
	}
	a.room.Overlay = next
	publish(a.room, effect)
	return nil
}
func actionGame(t *testing.T) (*Service, *actionSource, *testSink, string, *time.Time) {
	t.Helper()
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	a := &actionSource{runtimeSource: runtimeSource{room: domain.Room{Overlay: domain.Seed(world.Room{ID: "room", Version: "v1", Guides: []world.Guide{{ID: "g", Position: player.Vector{Z: 2}}}})}}}
	s.ConfigureRuntime(a)
	s.ConfigureGuideActions(a)
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	return s, a, sink, id, &now
}
func actionCommand() Command {
	return Command{Type: "guide.action", RequestID: "action", GuideID: "g", Text: "покажи макет", MovementEpoch: 1, ExpectedRevision: 1}
}
func TestGuideActionAdmissionAndPublication(t *testing.T) {
	s, a, sink, id, _ := actionGame(t)
	cmd := actionCommand()
	if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "GAME_NOT_READY" || a.calls != 0 {
		t.Fatal(f, a.calls)
	}
	s.Handle(t.Context(), "a", id, readyCommand())
	cmd.MovementEpoch = 2
	if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "STALE_MOVEMENT_EPOCH" || a.calls != 0 {
		t.Fatal(f, a.calls)
	}
	cmd.MovementEpoch = 1
	observer := &testSink{}
	attach(t, s, "b", observer)
	if f := s.Handle(t.Context(), "a", id, cmd); f.Code != "" {
		t.Fatal(f)
	}
	e := sink.events[len(sink.events)-1]
	other := observer.events[len(observer.events)-1]
	if e.Type != "guide.action.completed" || e.RequestID != "action" || e.GuideID != "g" || e.GuideEffect != domain.ShowInstallation || e.Runtime.Revision != 2 || other.Type != "world.runtime" || other.RequestID != "" || other.Runtime.Revision != 2 {
		t.Fatalf("%+v %+v", e, other)
	}
	a.before = func() { s.closeCurrent("a", id, 1011) }
	cmd.ExpectedRevision = 2
	if f := s.Handle(t.Context(), "a", id, cmd); !f.Drop || a.room.Revision != 2 {
		t.Fatal(f, a.room)
	}
}
func TestGuideActionRateDedupAndReconnect(t *testing.T) {
	s, _, _, id, now := actionGame(t)
	cmd := actionCommand()
	if f := s.Check("a", id, cmd); f.Code != "" {
		t.Fatal(f)
	}
	if f := s.Check("a", id, cmd); f.Code != "DUPLICATE_REQUEST" {
		t.Fatal(f)
	}
	for i := range 3 {
		cmd.RequestID = strconv.Itoa(i)
		f := s.Check("a", id, cmd)
		if i < 2 && f.Code != "" || i == 2 && (f.Code != "RATE_LIMITED" || f.RetryAfterMS != 10000) {
			t.Fatal(i, f)
		}
	}
	id = attach(t, s, "a", &testSink{})
	cmd.RequestID = "reconnect"
	if f := s.Check("a", id, cmd); f.Code != "RATE_LIMITED" {
		t.Fatal(f)
	}
	if f := s.AdmitNewAsk("a", id); f.Code != "" {
		t.Fatal("action consumed question quota", f)
	}
	*now = now.Add(10 * time.Second)
	cmd.RequestID = "later"
	if f := s.Check("a", id, cmd); f.Code != "" {
		t.Fatal(f)
	}
}
func TestGuideActionFailureDoesNotPublish(t *testing.T) {
	for _, failure := range []error{auth.Unavailable, domain.RevisionConflict, domain.OutOfRange, domain.UnsupportedAction} {
		s, a, sink, id, _ := actionGame(t)
		s.Handle(t.Context(), "a", id, readyCommand())
		a.err = failure
		before := len(sink.events)
		if f := s.Handle(t.Context(), "a", id, actionCommand()); f.Code == "" || len(sink.events) != before || a.room.Revision != 1 {
			t.Fatal(f, sink.events)
		}
	}
}
