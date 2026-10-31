package game

import (
	"context"
	"errors"
	"testing"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type gameCatalog struct{}

func (gameCatalog) Package(id string) (worlddomain.RoomPackage, error) {
	return worlddomain.RoomPackage{RoomID: id}, nil
}
func (gameCatalog) Geometry(id string) (worlddomain.Room, error) {
	return worlddomain.Room{ID: id, Min: player.Vector{X: -10, Z: -10}, Max: player.Vector{X: 10, Y: 3, Z: 10}, Doors: []worlddomain.Door{{ID: "door", Target: "next", Position: player.Vector{}, Radius: 1.5}}}, nil
}
func (gameCatalog) Spawn(string) (player.State, error) {
	return player.State{WorldVersion: "v1", RoomID: "next"}, nil
}

type poseStore struct {
	state player.State
	fail  bool
	saves int
}

func (p *poseStore) PlayerState(context.Context, string) (player.State, error) { return p.state, nil }
func (p *poseStore) SaveState(_ context.Context, _ authdomain.Session, old, next player.State, _ bool) (player.State, error) {
	if p.fail || !old.UpdatedAt.Equal(p.state.UpdatedAt) {
		return player.State{}, errors.New("save failed")
	}
	p.saves++
	next.UpdatedAt = p.state.UpdatedAt.Add(time.Microsecond)
	p.state = next
	return next, nil
}
func setupGame(t *testing.T) (*Service, *testSink, string, *time.Time, *poseStore) {
	t.Helper()
	now := time.Unix(100, 0)
	s := New(func() time.Time { return now })
	st := &poseStore{state: identity("a").State}
	s.Configure(gameCatalog{}, st, NoQuestions{})
	s.ConfigureSocial(nil, UnavailablePhotos{}, func() string { return "test-message" })
	sink := &testSink{}
	id := attach(t, s, "a", sink)
	return s, sink, id, &now, st
}
func readyCommand() Command {
	return Command{Type: "world.ready", RequestID: "ready", WorldVersion: "v1", MovementEpoch: 1}
}
func moveCommand(seq int64, x float64) Command {
	return Command{Type: "player.move", MovementEpoch: 1, Seq: seq, Position: player.Vector{X: x}, Rotation: player.Rotation{Yaw: 1}}
}
func TestReadyMovementSequenceAndIdle(t *testing.T) {
	s, sink, id, now, _ := setupGame(t)
	ctx := context.Background()
	if f := s.Handle(ctx, "a", id, moveCommand(1, .1)); f.Code != "GAME_NOT_READY" {
		t.Fatal(f)
	}
	if f := s.Handle(ctx, "a", id, readyCommand()); f.Code != "" {
		t.Fatal(f)
	}
	*now = now.Add(100 * time.Millisecond)
	s.Handle(ctx, "a", id, moveCommand(1, .4))
	s.Handle(ctx, "a", id, moveCommand(1, 2))
	s.PublishState()
	got := sink.events[len(sink.events)-1].Players[0]
	if got.State.Position.X != .4 || got.AckSeq != 1 {
		t.Fatal(got)
	}
	*now = now.Add(time.Hour)
	s.Handle(ctx, "a", id, readyCommand())
	s.Handle(ctx, "a", id, moveCommand(2, 2))
	s.PublishState()
	got = sink.events[len(sink.events)-1].Players[0]
	if got.State.Position.X != .4 || got.AckSeq != 2 || got.State.Rotation.Yaw != 1 {
		t.Fatal(got)
	}
	// Rejected move consumes server time: an immediate retry has no speed budget.
	s.Handle(ctx, "a", id, moveCommand(3, .5))
	s.PublishState()
	got = sink.events[len(sink.events)-1].Players[0]
	if got.State.Position.X != .4 || got.AckSeq != 3 {
		t.Fatal(got)
	}
	old := moveCommand(4, 0)
	old.MovementEpoch = 0
	if f := s.Handle(ctx, "a", id, old); !f.Drop {
		t.Fatal(f)
	}
	old.MovementEpoch = 2
	if f := s.Handle(ctx, "a", id, old); f.Code != "VALIDATION_ERROR" {
		t.Fatal(f)
	}
}
func TestDoorSaveBeforeEnteredAndReplacement(t *testing.T) {
	s, sink, id, _, store := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	cmd := Command{Type: "world.enter", RequestID: "enter", DoorID: "door", MovementEpoch: 1}
	store.fail = true
	if f := s.Handle(ctx, "a", id, cmd); !f.Drop || sink.closed != 1011 {
		t.Fatal(f, sink.closed)
	}
	if store.state.RoomID != "room" {
		t.Fatal("failed save changed persisted room")
	}
	for _, e := range sink.events {
		if e.Type == "world.entered" {
			t.Fatal("failed save confirmed a transition")
		}
	}
	store.fail = false
	sink = &testSink{}
	id = attach(t, s, "a", sink)
	s.Handle(ctx, "a", id, readyCommand())
	if f := s.Handle(ctx, "a", id, cmd); f.Code != "" {
		t.Fatal(f)
	}
	entered := sink.events[len(sink.events)-1]
	if entered.Type != "world.entered" || entered.Player.State.RoomID != "next" || entered.Player.MovementEpoch != 2 || store.saves != 1 {
		t.Fatal(entered, store.saves)
	}
	if f := s.Handle(ctx, "a", id, cmd); f.Code != "GAME_NOT_READY" {
		t.Fatal(f)
	}
	replacement := &testSink{}
	newID := attach(t, s, "a", replacement)
	if got := replacement.events[0].Snapshot; got.Self.State.RoomID != "next" || got.CurrentRoom.RoomID != "next" || got.Self.MovementEpoch != 1 {
		t.Fatal(got)
	}
	s.Detach("a", id)
	if !s.Current("a", newID) {
		t.Fatal("old cleanup changed owner")
	}
}
func TestPeriodicSaveFailureClosesCurrent(t *testing.T) {
	s, sink, id, _, store := setupGame(t)
	store.fail = true
	if err := s.SavePositions(context.Background()); err == nil {
		t.Fatal("save succeeded")
	}
	if sink.closed != 1011 || s.Current("a", id) {
		t.Fatal(sink.closed)
	}
}

type blockedStore struct {
	poseStore
	started, resume chan struct{}
}

func (b *blockedStore) SaveState(ctx context.Context, s authdomain.Session, old, next player.State, required bool) (player.State, error) {
	close(b.started)
	select {
	case <-ctx.Done():
		return player.State{}, ctx.Err()
	case <-b.resume:
	}
	return b.poseStore.SaveState(ctx, s, old, next, required)
}
func TestTransitionSerializesReplacementWithoutBlockingState(t *testing.T) {
	s, sink, id, _, store := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	blocked := &blockedStore{poseStore: *store, started: make(chan struct{}), resume: make(chan struct{})}
	s.store = blocked
	done := make(chan Failure, 1)
	go func() { done <- s.Handle(ctx, "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}) }()
	<-blocked.started
	// Snapshot broadcasts can proceed while this user's SQL write waits.
	s.PublishState()
	if got := sink.events[len(sink.events)-1].Players[0]; got.State.RoomID != "room" {
		t.Fatal(got)
	}
	ticket, err := s.Reserve(identity("a"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := &testSink{}
	attached := make(chan error, 1)
	go func() { attached <- s.Attach(ticket, replacement) }()
	close(blocked.resume)
	if f := <-done; f.Code != "" {
		t.Fatal(f)
	}
	if err = <-attached; err != nil {
		t.Fatal(err)
	}
	if got := replacement.events[0].Snapshot; got.Self.State.RoomID != "next" || got.CurrentRoom.RoomID != "next" {
		t.Fatal(got)
	}
	s.Detach("a", id)
	if blocked.saves != 1 {
		t.Fatal("old cleanup saved over replacement")
	}
}

type failingQuestions struct{}

func (failingQuestions) CancelQuestion(context.Context, string, string) error {
	return errors.New("cancel failed")
}
func TestCancelFailureKeepsRoomAndPose(t *testing.T) {
	s, sink, id, _, store := setupGame(t)
	s.questions = failingQuestions{}
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	n := len(sink.events)
	if f := s.Handle(ctx, "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}); f.Code != "SERVICE_UNAVAILABLE" {
		t.Fatal(f)
	}
	if store.saves != 0 || len(sink.events) != n {
		t.Fatal("transition published despite cancellation error")
	}
	if room, _ := s.OwningRoom(ctx, "session-a", "a"); room != "room" {
		t.Fatal(room)
	}
}
func TestDisconnectFlushAndMalformedMove(t *testing.T) {
	s, sink, id, now, store := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	*now = now.Add(time.Second)
	bad := moveCommand(1, .5)
	bad.Position.Y = 1
	if f := s.Handle(ctx, "a", id, bad); f.Code != "VALIDATION_ERROR" {
		t.Fatal(f)
	}
	s.PublishState()
	if got := sink.events[len(sink.events)-1].Players[0]; got.AckSeq != 0 || got.State.Rotation.Yaw != 0 {
		t.Fatal(got)
	}
	s.Handle(ctx, "a", id, moveCommand(2, .5))
	s.Detach("a", id)
	if store.state.Position.X != .5 || store.saves != 1 {
		t.Fatal(store.state, store.saves)
	}
}
func TestRepeatedReadyDoesNotResetMoveBudget(t *testing.T) {
	s, sink, id, now, _ := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	*now = now.Add(100 * time.Millisecond)
	s.Handle(ctx, "a", id, readyCommand())
	s.Handle(ctx, "a", id, moveCommand(1, .4))
	s.PublishState()
	if got := sink.events[len(sink.events)-1].Players[0]; got.State.Position.X != .4 {
		t.Fatal(got)
	}
}

func TestCancelledCommandCannotChangeReady(t *testing.T) {
	s, sink, id, _, _ := setupGame(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		if f := s.Handle(ctx, "a", id, readyCommand()); !f.Drop {
			t.Fatal("cancelled command was processed", f)
		}
	}
	if len(sink.events) != 1 {
		t.Fatal("cancelled ready emitted an acknowledgement")
	}
}
func TestSaveGateTimeoutClosesCurrent(t *testing.T) {
	s, sink, id, _, _ := setupGame(t)
	release, err := s.acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = s.SavePositions(ctx); err == nil {
		t.Fatal("busy save succeeded")
	}
	if sink.closed != 1011 || s.Current("a", id) {
		t.Fatal("timed-out save left unsaved game running", sink.closed)
	}
}

type noticeSink struct{ events chan Event }

func (n noticeSink) Enqueue(e Event) bool {
	select {
	case n.events <- e:
		return true
	default:
		return false
	}
}
func (noticeSink) Close(int) {}

type noticeStore struct {
	poseStore
	saved chan struct{}
}

func (n *noticeStore) SaveState(ctx context.Context, s authdomain.Session, old, next player.State, require bool) (player.State, error) {
	state, err := n.poseStore.SaveState(ctx, s, old, next, require)
	if err == nil {
		select {
		case n.saved <- struct{}{}:
		default:
		}
	}
	return state, err
}
func TestRunPublishesStateAndSavesPeriodically(t *testing.T) {
	store := &noticeStore{poseStore: poseStore{state: identity("a").State}, saved: make(chan struct{}, 1)}
	s := New(time.Now)
	s.Configure(gameCatalog{}, store, NoQuestions{})
	sink := noticeSink{events: make(chan Event, 100)}
	ticket, err := s.Reserve(identity("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Attach(ticket, sink); err != nil {
		t.Fatal(err)
	}
	<-sink.events
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case event := <-sink.events:
		if event.Type != "players.state" || len(event.Players) != 1 || event.Players[0].User.ID != "a" {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("no 10 Hz state")
	}
	select {
	case <-store.saved:
	case <-time.After(6 * time.Second):
		t.Fatal("no periodic save")
	}
}

func TestReplacementAfterSaveFailureReloadsDurableRevision(t *testing.T) {
	s, sink, _, _, store := setupGame(t)
	store.fail = true
	if err := s.SavePositions(context.Background()); err == nil {
		t.Fatal("save succeeded")
	}
	if sink.closed != 1011 {
		t.Fatal(sink.closed)
	}
	store.fail = false
	store.state.Position.X = 3
	store.state.UpdatedAt = store.state.UpdatedAt.Add(time.Microsecond)
	replacement := &testSink{}
	attach(t, s, "a", replacement)
	if got := replacement.events[0].Snapshot.Self.State; got.Position.X != 3 || !got.UpdatedAt.Equal(store.state.UpdatedAt) {
		t.Fatal("reused a failed live revision", got)
	}
}

type cancellingQuestions struct{ cancel context.CancelFunc }

func (q cancellingQuestions) CancelQuestion(context.Context, string, string) error {
	q.cancel()
	return nil
}
func TestCancelledReplacementDoesNotReplaceOwner(t *testing.T) {
	s, _, old, _, _ := setupGame(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.questions = cancellingQuestions{cancel: cancel}
	ticket, err := s.Reserve(identity("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AttachContext(ctx, ticket, &testSink{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled replacement attached", err)
	}
	if !s.Current("a", old) {
		t.Fatal("cancelled replacement removed owner")
	}
	s.Release(ticket)
}

type uncertainCommitStore struct{ poseStore }

func (p *uncertainCommitStore) SaveState(ctx context.Context, s authdomain.Session, old, next player.State, require bool) (player.State, error) {
	_, err := p.poseStore.SaveState(ctx, s, old, next, require)
	if err != nil {
		return player.State{}, err
	}
	return player.State{}, errors.New("connection lost after commit")
}
func TestReplacementAfterUncertainTransitionReloadsCommittedRoom(t *testing.T) {
	s, _, id, _, store := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	uncertain := &uncertainCommitStore{poseStore: *store}
	s.store = uncertain
	if f := s.Handle(ctx, "a", id, Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}); !f.Drop {
		t.Fatal(f)
	}
	if room, err := s.OwningRoom(ctx, "session-a", "a"); err == nil {
		t.Fatal("unconfirmed transition retained ownership", room)
	}
	replacement := &testSink{}
	attach(t, s, "a", replacement)
	if got := replacement.events[0].Snapshot; got.Self.State.RoomID != "next" || got.CurrentRoom.RoomID != "next" {
		t.Fatal("replacement ignored committed room", got)
	}
}
func TestSuccessfulTransitionClearsReloadBeforeNewMovement(t *testing.T) {
	s, _, id, now, store := setupGame(t)
	ctx := context.Background()
	s.Handle(ctx, "a", id, readyCommand())
	cmd := Command{Type: "world.enter", DoorID: "door", MovementEpoch: 1}
	store.fail = true
	s.Handle(ctx, "a", id, cmd)
	store.fail = false
	id = attach(t, s, "a", &testSink{})
	s.Handle(ctx, "a", id, readyCommand())
	if f := s.Handle(ctx, "a", id, cmd); f.Code != "" {
		t.Fatal(f)
	}
	ready := readyCommand()
	ready.MovementEpoch = 2
	s.Handle(ctx, "a", id, ready)
	*now = now.Add(time.Second)
	move := moveCommand(1, .5)
	move.MovementEpoch = 2
	s.Handle(ctx, "a", id, move)
	replacement := &testSink{}
	attach(t, s, "a", replacement)
	if got := replacement.events[0].Snapshot.Self.State; got.Position.X != .5 {
		t.Fatal("lost accepted movement after recovery", got)
	}
}
