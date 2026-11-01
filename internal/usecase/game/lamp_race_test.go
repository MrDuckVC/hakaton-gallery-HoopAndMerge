package game

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	runtimecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/runtime"
)

type lampRaceStore struct {
	mu               sync.Mutex
	overlay          domain.Overlay
	exists           bool
	entered, proceed chan struct{}
	block            bool
}

func (s *lampRaceStore) Read(context.Context, string, string) (domain.Overlay, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlay.Clone(), s.exists, nil
}
func (s *lampRaceStore) CompareAndSwap(context.Context, int64, domain.Overlay) error {
	return domain.ObjectBusy
}
func (s *lampRaceStore) IsAdmin(context.Context, string) (bool, error) { return false, nil }
func (s *lampRaceStore) CompareAndSwapLamp(_ context.Context, expected int64, h domain.Holder, next domain.Overlay) error {
	s.mu.Lock()
	block := s.block
	s.block = false
	s.mu.Unlock()
	if block {
		close(s.entered)
		<-s.proceed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rev := int64(1)
	old := domain.Holder{}
	if s.exists {
		rev = s.overlay.Revision
		old = s.overlay.Lamp.Owner()
	}
	if rev != expected || old != h {
		return domain.RevisionConflict
	}
	s.overlay = next.Clone()
	s.exists = true
	return nil
}
func TestLampDisconnectDuringCommitCannotAffectReplacement(t *testing.T) {
	registry := New(time.Now)
	st := &lampRaceStore{entered: make(chan struct{}), proceed: make(chan struct{})}
	rooms := runtimecase.New(recoveryCatalog{}, st, st, registry, recoverySizer{})
	registry.ConfigureRuntime(rooms)
	registry.ConfigureObjects(rooms)
	if err := rooms.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	oldSink := &testSink{}
	i := identity("a")
	i.State.Position = player.Vector{X: 3, Z: 4}
	old, err := registry.Reserve(i)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Attach(old, oldSink); err != nil {
		t.Fatal(err)
	}
	if f := registry.Handle(t.Context(), "a", old, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	st.block = true
	acted := make(chan Failure, 1)
	go func() { acted <- registry.Handle(t.Context(), "a", old, takeCommand(1)) }()
	select {
	case <-st.entered:
	case <-time.After(time.Second):
		t.Fatal("commit not reached")
	}
	detached := make(chan struct{})
	go func() { defer close(detached); registry.Detach("a", old) }()
	deadline := time.Now().Add(time.Second)
	for registry.Current("a", old) {
		if time.Now().After(deadline) {
			t.Fatal("disconnect did not revoke immediately")
		}
		time.Sleep(time.Millisecond)
	}
	next, err := registry.Reserve(i)
	if err != nil {
		t.Fatal(err)
	}
	nextSink := &testSink{}
	attached := make(chan error, 1)
	go func() { attached <- registry.AttachContext(t.Context(), next, nextSink) }()
	close(st.proceed)
	<-acted
	<-detached
	if err = <-attached; err != nil {
		t.Fatal(err)
	}
	for _, e := range oldSink.events {
		if e.Type == "object.acted" {
			t.Fatal("revoked socket received success")
		}
	}
	restored, _, err := st.Read(t.Context(), "v1", "room")
	if err != nil || restored.Revision != 3 || restored.Lamp.HolderUserID != "" {
		t.Fatal("in-flight holder was not released", restored, err)
	}
	if f := registry.Handle(t.Context(), "a", next, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	if f := registry.Handle(t.Context(), "a", next, takeCommand(3)); f.Code != "" {
		t.Fatal(f)
	}
	registry.Detach("a", old)
	restored, _, _ = st.Read(t.Context(), "v1", "room")
	if restored.Lamp.HolderConnectionID != next || !registry.Current("a", next) {
		t.Fatal("late cleanup removed new holder")
	}
	if err = registry.StopAccepting(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = registry.ReleaseObjects(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, _, _ = st.Read(t.Context(), "v1", "room")
	if restored.Lamp.HolderUserID != "" {
		t.Fatal("shutdown retained holder")
	}
}

func TestLampShutdownWaitsForInFlightCommit(t *testing.T) {
	registry := New(time.Now)
	st := &lampRaceStore{entered: make(chan struct{}), proceed: make(chan struct{})}
	rooms := runtimecase.New(recoveryCatalog{}, st, st, registry, recoverySizer{})
	registry.ConfigureRuntime(rooms)
	registry.ConfigureObjects(rooms)
	if err := rooms.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	sink := &testSink{}
	i := identity("a")
	i.State.Position = player.Vector{X: 3, Z: 4}
	id, err := registry.Reserve(i)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Attach(id, sink); err != nil {
		t.Fatal(err)
	}
	if f := registry.Handle(t.Context(), "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	st.block = true
	acted := make(chan Failure, 1)
	go func() { acted <- registry.Handle(t.Context(), "a", id, takeCommand(1)) }()
	select {
	case <-st.entered:
	case <-time.After(time.Second):
		t.Fatal("commit not reached")
	}
	if err = registry.StopAccepting(t.Context()); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() { released <- registry.ReleaseObjects(t.Context()) }()
	select {
	case err = <-released:
		t.Fatal("shutdown bypassed room gate", err)
	default:
	}
	close(st.proceed)
	<-acted
	if err = <-released; err != nil {
		t.Fatal(err)
	}
	restored, _, err := st.Read(t.Context(), "v1", "room")
	if err != nil || restored.Lamp.HolderUserID != "" || restored.Revision != 3 {
		t.Fatal(restored, err)
	}
	for _, e := range sink.events {
		if e.Type == "object.acted" {
			t.Fatal("shutdown acknowledged action")
		}
	}
}
