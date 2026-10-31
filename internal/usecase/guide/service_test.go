package guide

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

type memoryStore struct {
	records map[string]domain.Generation
	fail    bool
	failGet bool
	getHook func(context.Context) error
	memory  int
}

func (s *memoryStore) ByRequest(_ context.Context, u, r string) (domain.Generation, error) {
	for _, g := range s.records {
		if g.UserID == u && strings.EqualFold(g.RequestID, r) {
			return g, nil
		}
	}
	return domain.Generation{}, auth.Code("NOT_FOUND")
}
func (s *memoryStore) Create(_ context.Context, _ auth.Session, g domain.Generation) (domain.Generation, error) {
	if s.fail {
		return g, auth.Unavailable
	}
	s.records[g.ID] = g
	return g, nil
}
func (s *memoryStore) Claim(_ context.Context, now time.Time) (domain.Generation, error) {
	for id, g := range s.records {
		if g.Status == "queued" {
			g.Status = "running"
			g.StartedAt = &now
			s.records[id] = g
			return g, nil
		}
	}
	return domain.Generation{}, auth.Code("NOT_FOUND")
}
func (s *memoryStore) Pending(context.Context) ([]domain.Generation, error) {
	if s.fail {
		return nil, auth.Unavailable
	}
	r := make([]domain.Generation, 0, len(s.records))
	for _, g := range s.records {
		if !g.Terminal() {
			r = append(r, g)
		}
	}
	return r, nil
}
func (s *memoryStore) Finish(_ context.Context, g domain.Generation) (domain.Generation, bool, error) {
	if s.fail {
		return g, false, auth.Unavailable
	}
	old := s.records[g.ID]
	if old.Terminal() {
		return old, false, nil
	}
	s.records[g.ID] = g
	if g.Status == "completed" {
		s.memory++
	}
	return g, true, nil
}
func (s *memoryStore) Get(ctx context.Context, u, id string) (domain.Generation, error) {
	if s.getHook != nil {
		if err := s.getHook(ctx); err != nil {
			return domain.Generation{}, err
		}
	}
	if s.failGet {
		return domain.Generation{}, auth.Unavailable
	}
	g, ok := s.records[id]
	if !ok || g.UserID != u {
		return g, auth.Code("NOT_FOUND")
	}
	return g, nil
}
func (s *memoryStore) History(context.Context, string, string, int, string) ([]domain.Generation, string, error) {
	return nil, "", nil
}
func (s *memoryStore) Recover(_ context.Context, now time.Time) error {
	if s.fail {
		return auth.Unavailable
	}
	for id, g := range s.records {
		if !g.Terminal() {
			g.Status = "failed"
			g.Error = domain.Failure("SERVER_RESTARTED")
			g.FinishedAt = &now
			s.records[id] = g
		}
	}
	return nil
}

type guideWorld struct{}

func (guideWorld) Geometry(string) (world.Room, error) {
	return world.Room{Guides: []world.Guide{{ID: "guide"}}}, nil
}
func (guideWorld) HasGuide(id string) bool { return id == "guide" }

type eventLog struct {
	mu     sync.Mutex
	items  []Event
	closed int
	accept bool
}

func (l *eventLog) Emit(_, _ string, e Event) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, e)
	return l.accept
}
func (l *eventLog) CloseGuideConnection(_, _ string, _ int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed++
}

type modelFunc func(context.Context, domain.Generation, func(string) error) error

func (f modelFunc) Stream(c context.Context, g domain.Generation, e func(string) error) error {
	return f(c, g, e)
}
func fixture() (*Service, *memoryStore, *eventLog, Input) {
	store := &memoryStore{records: map[string]domain.Generation{}}
	events := &eventLog{accept: true}
	n := 0
	s := New(store, guideWorld{}, nil, events, UnavailableModel{}, Clock{Now: time.Now, After: time.After}, func() string { n++; return fmt.Sprintf("id-%d", n) })
	return s, store, events, Input{Session: auth.Session{UserID: "user"}, ConnectionID: "c1", Ready: true}
}
func TestAskReplayBeforeReadyAndLimits(t *testing.T) {
	s, store, events, in := fixture()
	charged := 0
	charge := func() Failure { charged++; return Failure{} }
	if f := s.Ask(context.Background(), in, "UUID", "guide", " question ", "", charge); f.Code != "" {
		t.Fatal(f)
	}
	in.Ready = false
	if f := s.Ask(context.Background(), in, "uuid", "guide", "question", "", charge); f.Code != "" {
		t.Fatal(f)
	}
	if charged != 1 || len(store.records) != 1 || len(events.items) != 2 {
		t.Fatal(charged, store.records, events.items)
	}
	if f := s.Ask(context.Background(), in, "uuid", "guide", "changed", "", charge); f.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal(f)
	}
	if f := s.Ask(context.Background(), in, "new", "guide", "question", "", charge); f.Code != "GAME_NOT_READY" {
		t.Fatal(f)
	}
}
func TestCancelCorrelationAndTerminalReplay(t *testing.T) {
	s, store, events, in := fixture()
	ctx := context.Background()
	s.Ask(ctx, in, "ask", "guide", "q", "", func() Failure { return Failure{} })
	id := events.items[0].Generation.ID
	if f := s.Cancel(ctx, in, "cancel", id); f.Code != "" {
		t.Fatal(f)
	}
	if len(events.items) != 3 || events.items[1].Type != "guide.failed" || events.items[1].RequestID != "ask" || events.items[2].RequestID != "cancel" {
		t.Fatal(events.items)
	}
	s.Cancel(ctx, in, "cancel-again", id)
	if len(events.items) != 4 || store.memory != 0 {
		t.Fatal(events.items)
	}
	foreign := in
	foreign.Session.UserID = "other"
	if f := s.Cancel(ctx, foreign, "bad", id); f.Code != "NOT_FOUND" {
		t.Fatal(f)
	}
}
func TestStreamAndStorageFailures(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		modelErr         error
		storage          bool
	}{
		{"complete", "hello", "completed", nil, false},
		{"unavailable", "partial", "AI_UNAVAILABLE", errors.New("model"), false},
		{"storage", "hello", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, events, in := fixture()
			ctx := context.Background()
			s.Ask(ctx, in, "ask", "guide", "q", "", func() Failure { return Failure{} })
			s.model = modelFunc(func(_ context.Context, _ domain.Generation, emit func(string) error) error {
				if err := emit(tc.text); err != nil {
					return err
				}
				store.fail = tc.storage
				return tc.modelErr
			})
			g, err := store.Claim(ctx, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			s.execute(ctx, g)
			if tc.storage {
				if events.closed != 1 {
					t.Fatal("missing close")
				}
				for _, e := range events.items {
					if e.Type == "guide.completed" {
						t.Fatal("false success")
					}
				}
				return
			}
			final := store.records[g.ID]
			if tc.want == "completed" {
				if final.Status != "completed" || final.AssistantMessage.Text != "hello" || store.memory != 1 {
					t.Fatal(final)
				}
			} else if final.Error.Code != tc.want || final.AssistantMessage != nil || store.memory != 0 {
				t.Fatal(final)
			}
			if events.items[0].Type != "guide.accepted" || events.items[1].Type != "guide.started" || events.items[2].Index != 0 {
				t.Fatal(events.items)
			}
		})
	}
}

func TestOutputLimitAndQueueDeadline(t *testing.T) {
	for _, size := range []int{8000, 8001} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s, store, events, in := fixture()
			ctx := context.Background()
			s.Ask(ctx, in, "ask", "guide", "q", "", func() Failure { return Failure{} })
			s.model = modelFunc(func(_ context.Context, _ domain.Generation, emit func(string) error) error {
				return emit(strings.Repeat("界", size))
			})
			g, err := store.Claim(ctx, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			s.execute(ctx, g)
			final := store.records[g.ID]
			if size == 8000 {
				if final.Status != "completed" {
					t.Fatal(final)
				}
			} else if final.Error == nil || final.Error.Code != "AI_OUTPUT_LIMIT" || final.AssistantMessage != nil {
				t.Fatal(final)
			}
			if events.items[len(events.items)-1].Generation.ID != g.ID {
				t.Fatal(events.items)
			}
		})
	}
	s, store, events, in := fixture()
	now := time.Unix(1000, 0)
	s.clock.Now = func() time.Time { return now }
	s.Ask(context.Background(), in, "ask", "guide", "q", "", func() Failure { return Failure{} })
	id := events.items[0].Generation.ID
	now = now.Add(120*time.Second - time.Nanosecond)
	s.sweep(context.Background())
	if store.records[id].Status != "queued" {
		t.Fatal("early expiry")
	}
	now = now.Add(time.Nanosecond)
	s.sweep(context.Background())
	if store.records[id].Error.Code != "AI_TIMEOUT" || store.memory != 0 {
		t.Fatal(store.records[id])
	}
}
func TestRunningTimeoutAndCancellation(t *testing.T) {
	for _, action := range []string{"timeout", "cancel", "shutdown", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			s, store, events, in := fixture()
			ctx := context.Background()
			s.Ask(ctx, in, "ask", "guide", "q", "", func() Failure { return Failure{} })
			timer := make(chan time.Time, 1)
			timerCreated := make(chan time.Duration, 1)
			s.clock.After = func(d time.Duration) <-chan time.Time { timerCreated <- d; return timer }
			entered := make(chan struct{})
			s.model = modelFunc(func(c context.Context, _ domain.Generation, emit func(string) error) error {
				if err := emit("partial"); err != nil {
					return err
				}
				close(entered)
				<-c.Done()
				return c.Err()
			})
			g, err := store.Claim(ctx, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { defer close(done); s.execute(ctx, g) }()
			<-entered
			if duration := <-timerCreated; duration > 90*time.Second || duration < 89*time.Second {
				t.Fatal(duration)
			}
			switch action {
			case "timeout":
				timer <- time.Now()
			case "cancel":
				s.Cancel(ctx, in, "cancel", g.ID)
			case "shutdown":
				if err = s.CancelGenerations(ctx); err != nil {
					t.Fatal(err)
				}
			case "disconnect":
				if err = s.CancelQuestion(ctx, in.Session.UserID, in.ConnectionID); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("worker did not stop")
			}
			final := store.records[g.ID]
			want := "CANCELLED"
			if action == "timeout" {
				want = "AI_TIMEOUT"
			}
			if final.Error == nil || final.Error.Code != want || final.AssistantMessage != nil || store.memory != 0 {
				t.Fatal(final)
			}
			failures := 0
			for _, e := range events.items {
				if e.Type == "guide.failed" {
					failures++
				}
			}
			if failures != 1 {
				t.Fatal(events.items)
			}
		})
	}
}
func TestAcceptedFailureAndOutageRecovery(t *testing.T) {
	s, store, events, in := fixture()
	events.accept = false
	if f := s.Ask(context.Background(), in, "ask", "guide", "q", "", func() Failure { return Failure{} }); f.Code != "" {
		t.Fatal(f)
	}
	for _, g := range store.records {
		if g.Error == nil || g.Error.Code != "CANCELLED" {
			t.Fatal(g)
		}
	}
	events.accept = true
	store.fail = true
	if f := s.Ask(context.Background(), in, "new", "guide", "q", "", func() Failure { return Failure{} }); f.Code != "SERVICE_UNAVAILABLE" {
		t.Fatal(f)
	}
	store.fail = false
	if f := s.Ask(context.Background(), in, "new", "guide", "q", "", func() Failure { return Failure{} }); f.Code != "" {
		t.Fatal(f)
	}
}

func TestOriginalAskCorrelationSurvivesReplay(t *testing.T) {
	s, _, events, in := fixture()
	ctx := context.Background()
	s.Ask(ctx, in, "AbCd", "guide", "q", "", func() Failure { return Failure{} })
	if events.items[0].RequestID != "AbCd" || events.items[0].Generation.RequestID != "AbCd" {
		t.Fatal(events.items)
	}
	s.Ask(ctx, in, "abcd", "guide", "q", "", func() Failure { t.Fatal("replay charged"); return Failure{} })
	if events.items[1].RequestID != "abcd" || events.items[1].Generation.RequestID != "AbCd" {
		t.Fatal(events.items)
	}
}

func TestClaimedReadOutageRecoversWithoutBlockingQueue(t *testing.T) {
	s, store, events, in := fixture()
	ctx := context.Background()
	s.Ask(ctx, in, "ask", "guide", "q", "", func() Failure { return Failure{} })
	g, err := store.Claim(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store.failGet = true
	s.execute(ctx, g)
	if !s.dirty.Load() || events.closed != 1 {
		t.Fatal("claimed outage was ignored", s.dirty.Load(), events.closed)
	}
	store.failGet = false
	s.sweep(ctx)
	if saved := store.records[g.ID]; saved.Error == nil || saved.Error.Code != "SERVER_RESTARTED" {
		t.Fatal(saved)
	}
}

func TestOutageRecoveryNotifiesAllPendingOwners(t *testing.T) {
	s, store, events, in := fixture()
	ctx := context.Background()
	s.Ask(ctx, in, "ask-a", "guide", "q", "", func() Failure { return Failure{} })
	in.Session.UserID = "other"
	in.ConnectionID = "c2"
	s.Ask(ctx, in, "ask-b", "guide", "q", "", func() Failure { return Failure{} })
	s.dirty.Store(true)
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if len(events.items) != 4 {
		t.Fatal("pending owners were not notified", events.items)
	}
	for _, e := range events.items[2:] {
		if e.Type != "guide.failed" || e.Generation.Error.Code != "SERVER_RESTARTED" {
			t.Fatal(e)
		}
	}
	for _, g := range store.records {
		if !g.Terminal() {
			t.Fatal(g)
		}
	}
}

type geometryWorld struct{ room world.Room }

func (w geometryWorld) Geometry(string) (world.Room, error) { return w.room, nil }
func (geometryWorld) HasGuide(id string) bool               { return id == "guide" }
func TestNewQuestionGeometryAndAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, guide, exhibit, want string
		distance                   float64
		wall                       bool
	}{
		{"boundary", "guide", "ex", "", 3, false},
		{"far", "guide", "", "OUT_OF_RANGE", 3.00001, false},
		{"missing guide", "missing", "", "NOT_FOUND", 0, false},
		{"missing exhibit", "guide", "other", "NOT_FOUND", 0, false},
		{"occluded", "guide", "", "OUT_OF_RANGE", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, _, in := fixture()
			room := world.Room{Guides: []world.Guide{{ID: "guide", Position: player.Vector{X: tc.distance}}}, Exhibits: []world.Exhibit{{ID: "ex"}}}
			if tc.wall {
				room.Surfaces = []world.Surface{{Position: player.Vector{X: 1.5, Y: 1.65}, Size: player.Vector{X: .2, Y: 3, Z: 3}, Collidable: true}}
			}
			s.world = geometryWorld{room}
			charged := 0
			f := s.Ask(context.Background(), in, "ask", tc.guide, "q", tc.exhibit, func() Failure { charged++; return Failure{} })
			if f.Code != tc.want {
				t.Fatal(f)
			}
			if tc.want != "" && (charged != 0 || len(store.records) != 0) {
				t.Fatal("rejected geometry consumed admission", charged, store.records)
			}
		})
	}
	s, store, _, in := fixture()
	if f := s.Ask(context.Background(), in, "ask", "guide", "q", "", func() Failure { return Failure{Code: "RATE_LIMITED", RetryAfterMS: 123} }); f.Code != "RATE_LIMITED" || f.RetryAfterMS != 123 || len(store.records) != 0 {
		t.Fatal(f)
	}
}

func TestRunningDeadlineDuringDeltaReadIsNotStorageOutage(t *testing.T) {
	s, store, events, in := fixture()
	ctx := context.Background()
	s.Ask(ctx, in, "ask", "guide", "q", "", func() Failure { return Failure{} })
	g, err := store.Claim(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	timer := make(chan time.Time, 1)
	s.clock.After = func(time.Duration) <-chan time.Time { return timer }
	reads := 0
	store.getHook = func(c context.Context) error {
		reads++
		if reads == 2 {
			close(entered)
			<-c.Done()
			return auth.Unavailable
		}
		return nil
	}
	s.model = modelFunc(func(_ context.Context, _ domain.Generation, emit func(string) error) error { return emit("delta") })
	done := make(chan struct{})
	go func() { defer close(done); s.execute(ctx, g) }()
	<-entered
	timer <- time.Now()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout hung")
	}
	final := store.records[g.ID]
	if final.Error == nil || final.Error.Code != "AI_TIMEOUT" || events.closed != 0 || s.dirty.Load() {
		t.Fatal(final, events.closed, s.dirty.Load())
	}
}
