package game

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

func TestChatAvailableBeforeReadyAndCorrelatedOnlyToSender(t *testing.T) {
	s, a, id, _, _ := setupGame(t)
	b := &testSink{}
	attach(t, s, "b", b)
	if f := s.Handle(context.Background(), "a", id, Command{Type: "chat.send", RequestID: "chat-1", Text: " hello "}); f.Code != "" || f.Drop {
		t.Fatalf("chat failed: %+v", f)
	}
	for _, tc := range []struct {
		sink    *testSink
		request string
	}{{a, "chat-1"}, {b, ""}} {
		e := tc.sink.events[len(tc.sink.events)-1]
		if e.Type != "chat.message" || e.RequestID != tc.request {
			t.Fatalf("chat event: %+v", e)
		}
	}
}

type inspectionFake struct {
	calls          int
	session        authdomain.Session
	state          player.State
	err            error
	saved          time.Time
	block, entered chan struct{}
}

func (f *inspectionFake) RecordInspection(ctx context.Context, session authdomain.Session, state player.State, exhibit string) (time.Time, error) {
	f.calls++
	f.session = session
	f.state = state
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.block:
		case <-ctx.Done():
			return time.Time{}, ctx.Err()
		}
	}
	return f.saved, f.err
}

type photoFake struct {
	session authdomain.Session
	calls   int
	err     error
}

func (f *photoFake) PublishPhoto(_ context.Context, session authdomain.Session, id string) (PublishedPhoto, error) {
	f.session = session
	f.calls++
	return PublishedPhoto{ID: id, OwnerID: session.UserID, Published: true}, f.err
}

type socialCatalog struct {
	gameCatalog
	room worlddomain.Room
}

func (c socialCatalog) Geometry(string) (worlddomain.Room, error) { return c.room, nil }
func setupSocial(t *testing.T) (*Service, *testSink, string, *inspectionFake) {
	t.Helper()
	s, sink, id, _, _ := setupGame(t)
	s.world = socialCatalog{room: worlddomain.Room{ID: "room", Exhibits: []worlddomain.Exhibit{{ID: "ex", Position: player.Vector{X: 3, Y: 1.65}}}}}
	store := &inspectionFake{saved: time.Unix(1000, 0)}
	s.ConfigureSocial(store, UnavailablePhotos{}, func() string { return "message-1" })
	return s, sink, id, store
}
func TestInspectReadinessGeometryAndDurableResponse(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		ready    bool
		exhibit  string
		position player.Vector
		wall     bool
		failure  string
	}{
		{"not ready", false, "ex", player.Vector{X: 3, Y: 1.65}, false, "GAME_NOT_READY"},
		{"boundary", true, "ex", player.Vector{X: 3, Y: 1.65}, false, ""},
		{"far", true, "ex", player.Vector{X: 3.000001, Y: 1.65}, false, "OUT_OF_RANGE"},
		{"wrong room or absent", true, "other", player.Vector{X: 3, Y: 1.65}, false, "NOT_FOUND"},
		{"wall", true, "ex", player.Vector{X: 3, Y: 1.65}, true, "OUT_OF_RANGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, sink, id, store := setupSocial(t)
			room := worlddomain.Room{Exhibits: []worlddomain.Exhibit{{ID: "ex", Position: tc.position}}}
			if tc.wall {
				room.Surfaces = []worlddomain.Surface{{Collidable: true, Position: player.Vector{X: 1.5, Y: 1.5}, Size: player.Vector{X: 0.1, Y: 3, Z: 3}}}
			}
			s.world = socialCatalog{room: room}
			if tc.ready {
				s.Handle(ctx, "a", id, readyCommand())
			}
			f := s.Handle(ctx, "a", id, Command{Type: "exhibit.inspect", RequestID: "inspect-1", ExhibitID: tc.exhibit})
			if f.Code != tc.failure {
				t.Fatal(f)
			}
			if tc.failure != "" {
				if store.calls != 0 {
					t.Fatal("rejected inspection persisted")
				}
				return
			}
			e := sink.events[len(sink.events)-1]
			if e.Type != "exhibit.inspected" || e.RequestID != "inspect-1" || e.ExhibitID != "ex" || !e.InspectedAt.Equal(store.saved) || store.session.UserID != "a" || store.state.RoomID != "room" {
				t.Fatalf("%+v %+v", e, store)
			}
		})
	}
}
func TestInspectStorageFailureNeverConfirms(t *testing.T) {
	for _, err := range []error{errors.New("SQL failed"), authdomain.Unauthenticated} {
		s, sink, id, store := setupSocial(t)
		store.err = err
		s.Handle(context.Background(), "a", id, readyCommand())
		f := s.Handle(context.Background(), "a", id, Command{Type: "exhibit.inspect", ExhibitID: "ex"})
		if errors.Is(err, authdomain.Unauthenticated) {
			if !f.Drop || sink.closed != 4001 {
				t.Fatal(f, sink.closed)
			}
		} else if f.Code != "SERVICE_UNAVAILABLE" {
			t.Fatal(f)
		}
		for _, e := range sink.events {
			if e.Type == "exhibit.inspected" {
				t.Fatal("false success")
			}
		}
	}
}
func TestChatValidationAndPhotoPublication(t *testing.T) {
	for _, text := range []string{"", " \u2003 ", strings.Repeat("я", 501)} {
		s, sink, id, _, _ := setupGame(t)
		n := len(sink.events)
		if f := s.Handle(context.Background(), "a", id, Command{Type: "chat.send", Text: text}); f.Code != "VALIDATION_ERROR" || len(sink.events) != n {
			t.Fatal(f)
		}
	}
	s, sink, id, _ := setupSocial(t)
	for _, tc := range []struct{ text, photo, code string }{{strings.Repeat("я", 500), "", ""}, {"", "p1", "SERVICE_UNAVAILABLE"}} {
		f := s.Handle(context.Background(), "a", id, Command{Type: "chat.send", Text: tc.text, PhotoID: tc.photo})
		if f.Code != tc.code {
			t.Fatal(f)
		}
	}
	photos := &photoFake{}
	s.photos = photos
	if f := s.Handle(context.Background(), "a", id, Command{Type: "chat.send", PhotoID: "p1"}); f.Code != "" {
		t.Fatal(f)
	}
	e := sink.events[len(sink.events)-1]
	if e.Chat.Photo == nil || e.Chat.Photo.ID != "p1" || e.Chat.Text != "" || photos.session.UserID != "a" {
		t.Fatalf("%+v", e)
	}
	photos.err = authdomain.Code("NOT_FOUND")
	n := len(sink.events)
	if f := s.Handle(context.Background(), "a", id, Command{Type: "chat.send", Text: "hi", PhotoID: "p2"}); f.Code != "NOT_FOUND" || len(sink.events) != n {
		t.Fatal(f)
	}
}
func TestInspectDisconnectDuringSQLDropsSuccess(t *testing.T) {
	s, sink, id, store := setupSocial(t)
	s.Handle(context.Background(), "a", id, readyCommand())
	store.entered = make(chan struct{})
	store.block = make(chan struct{})
	result := make(chan Failure, 1)
	go func() {
		result <- s.Handle(context.Background(), "a", id, Command{Type: "exhibit.inspect", ExhibitID: "ex"})
	}()
	<-store.entered
	// Detach's first operation revokes ownership independently of the per-user SQL gate.
	detached := make(chan struct{})
	go func() { s.Detach("a", id); close(detached) }()
	deadline := time.After(time.Second)
	for s.Current("a", id) {
		select {
		case <-deadline:
			t.Fatal("detach blocked on SQL")
		default:
			runtime.Gosched()
		}
	}
	// Other users remain usable while the inspection waits.
	b := &testSink{}
	bid := attach(t, s, "b", b)
	if f := s.Handle(context.Background(), "b", bid, Command{Type: "chat.send", Text: "hi"}); f.Code != "" {
		t.Fatal(f)
	}
	close(store.block)
	if f := <-result; !f.Drop {
		t.Fatal(f)
	}
	<-detached
	for _, e := range sink.events {
		if e.Type == "exhibit.inspected" {
			t.Fatal("late success")
		}
	}
}

func TestChatSlidingWindowReconnectAndNoHistory(t *testing.T) {
	s, a, id, now, _ := setupGame(t)
	b := &testSink{}
	attach(t, s, "b", b)
	for i := range 5 {
		cmd := Command{Type: "chat.send", RequestID: strconv.Itoa(i), Text: "hello"}
		if f := s.Check("a", id, cmd); f.Code != "" {
			t.Fatal(f)
		}
		if f := s.Handle(context.Background(), "a", id, cmd); f.Code != "" {
			t.Fatal(f)
		}
	}
	last := a.events[len(a.events)-1]
	if f := s.Check("a", id, Command{Type: "chat.send", RequestID: "sixth"}); f.Code != "RATE_LIMITED" || f.RetryAfterMS != 10000 {
		t.Fatal(f)
	}
	replacement := &testSink{}
	id = attach(t, s, "a", replacement)
	if len(replacement.events) != 1 || replacement.events[0].Type != "world.snapshot" {
		t.Fatal("chat history replayed", replacement.events)
	}
	*now = now.Add(9999 * time.Millisecond)
	if f := s.Check("a", id, Command{Type: "chat.send", RequestID: "seven"}); f.Code != "RATE_LIMITED" || f.RetryAfterMS != 1 {
		t.Fatal(f)
	}
	*now = now.Add(time.Millisecond)
	if f := s.Check("a", id, Command{Type: "chat.send", RequestID: "eight"}); f.Code != "" {
		t.Fatal(f)
	}
	if last.Type != "chat.message" || last.Chat.Text != "hello" {
		t.Fatal(last)
	}
}
func TestInspectUsesCameraAndIgnoresNoncollidableSurfaces(t *testing.T) {
	for _, tc := range []struct {
		name       string
		collidable bool
		height     float64
	}{{"low wall below camera", true, 1}, {"noncollidable wall", false, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, id, _ := setupSocial(t)
			room := worlddomain.Room{Exhibits: []worlddomain.Exhibit{{ID: "ex", Position: player.Vector{X: 3, Y: 1.65}}}, Surfaces: []worlddomain.Surface{{Collidable: tc.collidable, Position: player.Vector{X: 1.5, Y: tc.height / 2}, Size: player.Vector{X: 0.1, Y: tc.height, Z: 3}}}}
			s.world = socialCatalog{room: room}
			s.Handle(context.Background(), "a", id, readyCommand())
			if f := s.Handle(context.Background(), "a", id, Command{Type: "exhibit.inspect", ExhibitID: "ex"}); f.Code != "" {
				t.Fatal(f)
			}
		})
	}
}
