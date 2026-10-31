package game

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
)

func TestDetachRevokesOwnershipBeforeFlushCompletes(t *testing.T) {
	s, _, id, _, store := setupGame(t)
	blocked := &blockedStore{poseStore: *store, started: make(chan struct{}), resume: make(chan struct{})}
	s.store = blocked
	observer := &testSink{}
	attach(t, s, "b", observer)
	done := make(chan struct{})
	go func() { defer close(done); s.Detach("a", id) }()
	<-blocked.started
	defer func() { close(blocked.resume); <-done }()
	if s.Current("a", id) {
		t.Error("disconnected connection is still current while flush waits")
	}
	if room, err := s.OwningRoom(context.Background(), "session-a", "a"); err == nil {
		t.Error("disconnected connection still owns room", room)
	}
	s.PublishState()
	state := observer.events[len(observer.events)-1]
	if state.Type != "players.state" || len(state.Players) != 1 || state.Players[0].User.ID != "b" {
		t.Error("state includes disconnected player", state)
	}
	newcomer := &testSink{}
	attach(t, s, "c", newcomer)
	for _, p := range newcomer.events[0].Snapshot.Players {
		if p.User.ID == "a" {
			t.Error("snapshot includes disconnected player")
		}
	}
}

func TestUncertainTransitionStopsCurrentGame(t *testing.T) {
	s, sink, id, now, store := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	uncertain := &uncertainCommitStore{poseStore: *store}
	s.store = uncertain
	s.Handle(ctx, "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1})
	if uncertain.state.RoomID != "next" {
		t.Fatal("test did not commit the transition")
	}
	if s.Current("a", id) || sink.closed != 1011 {
		t.Error("uncertain commit left the previous connection active", sink.closed)
	}
	if room, err := s.OwningRoom(ctx, "session-a", "a"); err == nil {
		t.Error("uncertain commit retained stale HTTP ownership", room)
	}
	*now = now.Add(time.Second)
	if f := s.Handle(ctx, "a", id, moveCommand(1, .5)); !f.Drop {
		t.Error("old room accepted a move after uncertain commit", f)
	}
	observer := &testSink{}
	attach(t, s, "b", observer)
	s.PublishState()
	for _, p := range observer.events[len(observer.events)-1].Players {
		if p.User.ID == "a" {
			t.Error("old room published after uncertain commit", p)
		}
	}
	replacement := &testSink{}
	attach(t, s, "a", replacement)
	if got := replacement.events[0].Snapshot; got.Self.State.RoomID != "next" || got.CurrentRoom.RoomID != "next" {
		t.Error("replacement did not recover committed room", got)
	}
}

type slowNeighbourStore struct {
	mu                       sync.Mutex
	states                   map[string]player.State
	saved                    chan player.State
	slowStarted, releaseSlow chan struct{}
	once                     sync.Once
}

func (p *slowNeighbourStore) PlayerState(_ context.Context, id string) (player.State, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.states[id], nil
}
func (p *slowNeighbourStore) SaveState(ctx context.Context, s authdomain.Session, old, next player.State, _ bool) (player.State, error) {
	if s.UserID == "slow" {
		p.once.Do(func() { close(p.slowStarted) })
		select {
		case <-ctx.Done():
			return player.State{}, ctx.Err()
		case <-p.releaseSlow:
			return player.State{}, errors.New("slow database failure")
		}
	}
	p.mu.Lock()
	if !p.states[s.UserID].UpdatedAt.Equal(old.UpdatedAt) {
		p.mu.Unlock()
		return player.State{}, errors.New("stale revision")
	}
	next.UpdatedAt = old.UpdatedAt.Add(time.Microsecond)
	p.states[s.UserID] = next
	p.mu.Unlock()
	p.saved <- next
	return next, nil
}
func TestPendingSaveTickPreservesHealthyPlayersNextPosition(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Unix(100, 0).UnixNano())
	s := New(func() time.Time { return time.Unix(0, clock.Load()) })
	store := &slowNeighbourStore{states: map[string]player.State{"fast": identity("fast").State, "slow": identity("slow").State}, saved: make(chan player.State, 10), slowStarted: make(chan struct{}), releaseSlow: make(chan struct{})}
	s.Configure(gameCatalog{}, store, NoQuestions{})
	fastSink := noticeSink{events: make(chan Event, 100)}
	ticket, err := s.Reserve(identity("fast"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(ticket, fastSink); err != nil {
		t.Fatal(err)
	}
	slowSink := noticeSink{events: make(chan Event, 100)}
	slowTicket, err := s.Reserve(identity("slow"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(slowTicket, slowSink); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stateTicks, saveTicks := make(chan time.Time), make(chan time.Time)
	done := make(chan struct{})
	go func() { defer close(done); s.run(ctx, stateTicks, saveTicks) }()
	defer func() { cancel(); <-done }()
	s.Handle(ctx, "fast", ticket, readyCommand())
	clock.Add(int64(100 * time.Millisecond))
	s.Handle(ctx, "fast", ticket, moveCommand(1, .4))
	saveTicks <- time.Unix(105, 0)
	select {
	case <-store.slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow save did not start")
	}
	select {
	case state := <-store.saved:
		if state.Position.X != .4 {
			t.Fatal(state)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy initial save missing")
	}
	clock.Add(int64(100 * time.Millisecond))
	s.Handle(ctx, "fast", ticket, moveCommand(2, .8))
	// Force the next tick to arrive before the previous global cycle completes.
	saveTicks <- time.Unix(110, 0)
	stateTicks <- time.Unix(110, 0)
	for {
		select {
		case e := <-fastSink.events:
			if e.Type == "players.state" {
				goto processed
			}
		case <-time.After(time.Second):
			t.Fatal("state tick blocked by SQL")
		}
	}
processed:
	close(store.releaseSlow)
	select {
	case state := <-store.saved:
		if state.Position.X != .8 {
			t.Fatal("pending save missed accepted movement", state)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy player's second save was lost behind slow neighbour")
	}
	if !s.Current("fast", ticket) {
		t.Fatal("healthy player was closed by slow neighbour failure")
	}
}

type cleanupQuestions struct {
	timeout bool
	err     error
	calls   int
}

func (q *cleanupQuestions) CancelQuestion(ctx context.Context, _, _ string) error {
	q.calls++
	if q.timeout {
		<-ctx.Done()
		return ctx.Err()
	}
	return q.err
}

type contextPoseStore struct {
	*poseStore
	saveErr error
	budget  time.Duration
}

func (p *contextPoseStore) SaveState(ctx context.Context, session authdomain.Session, old, next player.State, required bool) (player.State, error) {
	p.saveErr = ctx.Err()
	if p.saveErr != nil {
		return player.State{}, p.saveErr
	}
	if deadline, ok := ctx.Deadline(); ok {
		p.budget = time.Until(deadline)
	}
	return p.poseStore.SaveState(ctx, session, old, next, required)
}

func TestDetachSavesPositionAfterQuestionCancellation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout bool
		err     error
	}{
		{name: "deadline", timeout: true},
		{name: "failure", err: errors.New("guide storage unavailable")},
		{name: "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, _, id, now, store := setupGame(t)
				positions := &contextPoseStore{poseStore: store}
				questions := &cleanupQuestions{timeout: tc.timeout, err: tc.err}
				s.store = positions
				s.questions = questions
				s.Handle(t.Context(), "a", id, readyCommand())
				*now = now.Add(100 * time.Millisecond)
				s.Handle(t.Context(), "a", id, moveCommand(1, .4))
				started := time.Now()
				s.Detach("a", id)
				if questions.calls != 1 || positions.saveErr != nil || store.saves != 1 || store.state.Position.X != .4 {
					t.Fatalf("cancel calls=%d, save error=%v, saves=%d, durable x=%v", questions.calls, positions.saveErr, store.saves, store.state.Position.X)
				}
				if positions.budget <= 0 || positions.budget > 5*time.Second {
					t.Fatalf("invalid final save budget %v", positions.budget)
				}
				if elapsed := time.Since(started); elapsed > 10*time.Second {
					t.Fatalf("unbounded disconnect cleanup %v", elapsed)
				}
				replacement := &testSink{}
				newID := attach(t, s, "a", replacement)
				s.Detach("a", id)
				if !s.Current("a", newID) || questions.calls != 1 || store.saves != 1 {
					t.Fatal("old cleanup affected replacement")
				}
			})
		})
	}
}
