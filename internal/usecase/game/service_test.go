package game

import (
	"context"
	"strconv"
	"testing"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type testSink struct {
	events []Event
	closed int
}

func (s *testSink) Enqueue(e Event) bool { s.events = append(s.events, e); return true }
func (s *testSink) Close(code int)       { s.closed = code }
func identity(id string) Identity {
	return Identity{Session: authdomain.Session{ID: "session-" + id, UserID: id}, User: authdomain.User{ID: id}, State: player.State{RoomID: "room", WorldVersion: "v1"}, Package: worlddomain.RoomPackage{RoomID: "room"}}
}
func attach(t *testing.T, s *Service, id string, sink *testSink) string {
	t.Helper()
	ticket, err := s.Reserve(identity(id))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(ticket, sink); err != nil {
		t.Fatal(err)
	}
	return ticket
}
func TestSnapshotReplacementAndOwnership(t *testing.T) {
	s := New(time.Now)
	a, b, replacement := &testSink{}, &testSink{}, &testSink{}
	old := attach(t, s, "a", a)
	attach(t, s, "b", b)
	current := attach(t, s, "a", replacement)
	if a.closed != 4009 {
		t.Fatalf("old close=%d", a.closed)
	}
	if replacement.events[0].Type != "world.snapshot" || len(replacement.events[0].Snapshot.Players) != 1 {
		t.Fatal(replacement.events)
	}
	if n := len(b.events); b.events[n-2].Type != "player.left" || b.events[n-1].Type != "player.joined" {
		t.Fatal(b.events)
	}
	s.Detach("a", old)
	if room, err := s.OwningRoom(context.Background(), "session-a", "a"); err != nil || room != "room" {
		t.Fatal(room, err)
	}
	if _, err := s.OwningRoom(context.Background(), "other", "a"); err == nil {
		t.Fatal("foreign session owns room")
	}
	if s.Current("a", old) || !s.Current("a", current) {
		t.Fatal("stale identity accepted")
	}
	s.Detach("a", current)
	if _, err := s.OwningRoom(context.Background(), "session-a", "a"); err == nil {
		t.Fatal("detached owns room")
	}
}
func TestAdmissionReservations(t *testing.T) {
	s := New(time.Now)
	for i := range 32 {
		attach(t, s, strconv.Itoa(i), &testSink{})
	}
	if _, err := s.Reserve(identity("33")); err == nil {
		t.Fatal("33rd admitted")
	}
	ticket, err := s.Reserve(identity("0"))
	if err != nil {
		t.Fatal(err)
	}
	s.Release(ticket)
	if _, err = s.Reserve(identity("33")); err == nil {
		t.Fatal("released another reservation")
	}
	if err = s.StopAccepting(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(identity("0")); err == nil {
		t.Fatal("admitted during shutdown")
	}
}
func TestLimitsSurviveReconnectAndDedupResets(t *testing.T) {
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	sink := &testSink{}
	ticket := attach(t, s, "a", sink)
	if got := s.Check("a", ticket, Command{Type: "world.ready", RequestID: "one"}); got.Code != "" {
		t.Fatal(got)
	}
	if got := s.Check("a", ticket, Command{Type: "world.ready", RequestID: "one"}); got.Code != "DUPLICATE_REQUEST" {
		t.Fatal(got)
	}
	for i := range 29 {
		s.Check("a", ticket, Command{Type: "world.ready", RequestID: strconv.Itoa(i)})
	}
	ticket = attach(t, s, "a", &testSink{})
	if got := s.Check("a", ticket, Command{Type: "world.ready", RequestID: "one"}); got.Code != "RATE_LIMITED" || got.RetryAfterMS != 10000 {
		t.Fatal(got)
	}
	now = now.Add(10 * time.Second)
	if got := s.Check("a", ticket, Command{Type: "world.ready", RequestID: "fresh"}); got.Code != "" {
		t.Fatal(got)
	}
}
func TestMoveAbuseClosesAfterFiveSeconds(t *testing.T) {
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	sink := &testSink{}
	ticket := attach(t, s, "a", sink)
	for second := range 5 {
		for range 21 {
			s.Check("a", ticket, Command{Type: "player.move"})
		}
		if second < 4 && sink.closed != 0 {
			t.Fatal("closed too early")
		}
		now = now.Add(time.Second)
	}
	if sink.closed != 1008 {
		t.Fatalf("close=%d", sink.closed)
	}
}

func TestMoveAbuseSurvivesReconnect(t *testing.T) {
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	sink := &testSink{}
	ticket := attach(t, s, "a", sink)
	for second := range 5 {
		for range 21 {
			s.Check("a", ticket, Command{Type: "player.move"})
		}
		if second < 4 {
			now = now.Add(time.Second)
			sink = &testSink{}
			ticket = attach(t, s, "a", sink)
		}
	}
	if sink.closed != 1008 {
		t.Fatalf("reconnect reset abuse: close=%d", sink.closed)
	}
}
func TestCommandRetentionLimit(t *testing.T) {
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	sink := &testSink{}
	ticket := attach(t, s, "a", sink)
	for i := range 10000 {
		now = now.Add(time.Second)
		f := s.Check("a", ticket, Command{Type: "world.ready", RequestID: strconv.Itoa(i)})
		if f.Code != "" {
			t.Fatal(i, f)
		}
		if i < 9999 && sink.closed != 0 {
			t.Fatal("closed before 10000")
		}
	}
	if sink.closed != 1012 {
		t.Fatal(sink.closed)
	}
	if f := s.Check("a", ticket, Command{Type: "world.ready", RequestID: "extra"}); !f.Drop {
		t.Fatal("admitted beyond retention limit", f)
	}
}

func TestDuplicateCommandsConsumeAggregateWindow(t *testing.T) {
	s := New(time.Now)
	ticket := attach(t, s, "a", &testSink{})
	for range 30 {
		s.Check("a", ticket, Command{Type: "world.ready", RequestID: "same"})
	}
	if f := s.Check("a", ticket, Command{Type: "world.ready", RequestID: "same"}); f.Code != "RATE_LIMITED" {
		t.Fatal(f)
	}
}
