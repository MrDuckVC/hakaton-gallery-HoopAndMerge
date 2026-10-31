package game

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	guidecase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/guide"
)

// Configure installs game ports before admission starts.
func (s *Service) Configure(world World, store PositionStore, questions Questions) {
	s.world, s.store, s.questions = world, store, questions
}

// acquire serializes this user's operations without holding a mutex during I/O.
func (s *Service) acquire(ctx context.Context, userID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	gate := s.gates[userID]
	if gate == nil {
		gate = make(chan struct{}, 1)
		gate <- struct{}{}
		s.gates[userID] = gate
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
		if err := ctx.Err(); err != nil {
			gate <- struct{}{}
			return nil, err
		}
		return func() { gate <- struct{}{} }, nil
	}
}

// Handle owns game command semantics after transport authentication and rate admission.
func (s *Service) Handle(ctx context.Context, userID, id string, cmd Command) Failure {
	release, err := s.acquire(ctx, userID)
	if err != nil {
		return Failure{Drop: true}
	}
	defer release()
	s.mu.Lock()
	c := s.active[userID]
	if c == nil || c.id != id || c.ended || s.stopping {
		s.mu.Unlock()
		return Failure{Drop: true}
	}
	switch cmd.Type {
	case "world.ready":
		defer s.mu.Unlock()
		if cmd.WorldVersion != c.identity.State.WorldVersion {
			return Failure{Code: "WORLD_VERSION_MISMATCH"}
		}
		if cmd.MovementEpoch != c.epoch {
			return Failure{Code: "STALE_MOVEMENT_EPOCH"}
		}
		if !c.ready {
			c.ready = true
			c.lastMove = s.now()
		}
		c.sink.Enqueue(Event{Type: "world.ready", RequestID: cmd.RequestID, MovementEpoch: c.epoch})
		return Failure{}
	case "chat.send":
		s.mu.Unlock()
		return s.chat(ctx, c, cmd)
	case "guide.ask", "guide.cancel":
		in := guidecase.Input{Session: c.identity.Session, State: c.identity.State, ConnectionID: c.id, Ready: c.ready}
		s.mu.Unlock()
		return s.guideCommand(ctx, in, cmd)
	}
	if !c.ready {
		s.mu.Unlock()
		return Failure{Code: "GAME_NOT_READY"}
	}
	if cmd.Type == "player.move" {
		defer s.mu.Unlock()
		return s.moveLocked(c, cmd)
	}
	if cmd.Type == "exhibit.inspect" {
		state := c.identity.State
		s.mu.Unlock()
		return s.inspect(ctx, c, state, cmd)
	}
	if cmd.Type != "world.enter" {
		s.mu.Unlock()
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	if cmd.MovementEpoch != c.epoch {
		s.mu.Unlock()
		return Failure{Code: "STALE_MOVEMENT_EPOCH"}
	}
	if c.epoch >= worlddomain.MaxSafeInteger {
		s.mu.Unlock()
		s.closeCurrent(userID, id, 1012)
		return Failure{Drop: true}
	}
	state := c.identity.State
	s.mu.Unlock()
	if s.world == nil || s.store == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	room, err := s.world.Geometry(state.RoomID)
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	var door worlddomain.Door
	found := false
	for _, d := range room.Doors {
		if d.ID == cmd.DoorID {
			door = d
			found = true
			break
		}
	}
	if !found {
		return Failure{Code: "NOT_FOUND"}
	}
	camera := state.Position
	camera.Y += 1.65
	if math.Hypot(state.Position.X-door.Position.X, state.Position.Z-door.Position.Z) > 1.5 || !worlddomain.Visible(room, camera, door.Position) {
		return Failure{Code: "OUT_OF_RANGE"}
	}
	next, err := s.world.Spawn(door.ID)
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	pkg, err := s.world.Package(next.RoomID)
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	if err = s.questions.CancelQuestion(ctx, userID, id); err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	if err = ctx.Err(); err != nil {
		return Failure{Drop: true}
	}
	next, err = s.store.SaveState(ctx, c.identity.Session, state, next, true)
	if err != nil {
		s.mu.Lock()
		if s.active[userID] == c {
			c.reloadPose = true
		}
		s.mu.Unlock()
		// The port cannot prove rollback after a failed COMMIT response. Stop this
		// connection before any more moves or downloads can use the old room.
		code := 1011
		if errors.Is(err, authdomain.Unauthenticated) {
			code = 4001
		}
		s.closeCurrent(userID, id, code)
		return Failure{Drop: true}
	}
	// Ownership operations use the same user gate. Publish committed data even if
	// shutdown has started: the database is now the canonical position.
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.active[userID]; current == c {
		c.identity.State = next
		c.reloadPose = false
		c.identity.Package = pkg
		c.epoch++
		c.ack = 0
		c.ready = false
		c.lastMove = time.Time{}
		c.sink.Enqueue(Event{Type: "world.entered", RequestID: cmd.RequestID, Player: presenceConnection(c), CurrentRoom: pkg})
	}
	return Failure{}
}

func (s *Service) moveLocked(c *connection, cmd Command) Failure {
	if cmd.MovementEpoch < c.epoch {
		return Failure{Drop: true}
	}
	if cmd.MovementEpoch > c.epoch {
		return Failure{Code: "VALIDATION_ERROR"}
	}
	if cmd.Seq <= c.ack {
		return Failure{Drop: true}
	}
	p := cmd.Position
	if cmd.Seq < 1 || cmd.Seq > worlddomain.MaxSafeInteger || p.Y != 0 || math.IsNaN(p.X) || math.IsNaN(p.Z) || math.IsInf(p.X, 0) || math.IsInf(p.Z, 0) || !worlddomain.ValidRotation(cmd.Rotation) {
		return Failure{Code: "VALIDATION_ERROR"}
	}
	now := s.now()
	dt := max(0, min(now.Sub(c.lastMove).Seconds(), .25))
	c.lastMove = now
	c.ack = cmd.Seq
	c.identity.State.Rotation = cmd.Rotation
	if math.Hypot(p.X-c.identity.State.Position.X, p.Z-c.identity.State.Position.Z) > 4*dt+.000001 {
		return Failure{}
	}
	if s.world == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	room, err := s.world.Geometry(c.identity.State.RoomID)
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	if worlddomain.Sweep(room, c.identity.State.Position, p) {
		c.identity.State.Position = p
	}
	return Failure{}
}

// PublishState queues a complete, consistent view independently of persistence I/O.
func (s *Service) PublishState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	players := make([]Player, 0, len(s.active))
	for _, c := range s.active {
		if !c.ended {
			players = append(players, presenceConnection(c))
		}
	}
	sort.Slice(players, func(i, j int) bool { return players[i].User.ID < players[j].User.ID })
	for _, c := range s.active {
		if !c.ended {
			c.sink.Enqueue(Event{Type: "players.state", Players: players})
		}
	}
}

// flush is called under the user gate. It saves only already accepted server state.
func (s *Service) flush(ctx context.Context, userID, id string) error {
	if s.store == nil {
		return nil
	}
	s.mu.Lock()
	c := s.active[userID]
	if c == nil || c.id != id {
		s.mu.Unlock()
		return nil
	}
	before := c.identity.State
	session := c.identity.Session
	s.mu.Unlock()
	saved, err := s.store.SaveState(ctx, session, before, before, false)
	if err != nil {
		s.closeCurrent(userID, id, 1011)
		return err
	}
	s.mu.Lock()
	if s.active[userID] == c {
		c.identity.State.UpdatedAt = saved.UpdatedAt
		c.reloadPose = false
	}
	s.mu.Unlock()
	return nil
}

// SavePositions flushes users concurrently within the caller's shared deadline.
func (s *Service) SavePositions(ctx context.Context) error {
	s.mu.Lock()
	ids := make(map[string]string, len(s.active))
	for user, c := range s.active {
		ids[user] = c.id
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	failures := make(chan error, len(ids))
	for user, id := range ids {
		wg.Go(func() {
			release, err := s.acquire(ctx, user)
			if err == nil {
				defer release()
				err = s.flush(ctx, user, id)
			}
			if err != nil {
				s.closeCurrent(user, id, 1011)
				failures <- err
			}
		})
	}
	wg.Wait()
	close(failures)
	var result error
	for err := range failures {
		result = errors.Join(result, err)
	}
	return result
}

// Run owns bounded state/save ticks until cancellation. Each save cycle completes
// before the next begins; overlapping ticks coalesce into one deferred cycle.
func (s *Service) Run(ctx context.Context) {
	stateTick := time.NewTicker(100 * time.Millisecond)
	defer stateTick.Stop()
	saveTick := time.NewTicker(5 * time.Second)
	defer saveTick.Stop()
	s.run(ctx, stateTick.C, saveTick.C)
}

func (s *Service) run(ctx context.Context, stateTicks, saveTicks <-chan time.Time) {
	completed := make(chan struct{}, 1)
	running, pending := false, false
	var workers sync.WaitGroup
	defer workers.Wait()
	startSave := func() {
		running = true
		workers.Go(func() {
			defer func() { completed <- struct{}{} }()
			saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_ = s.SavePositions(saveCtx)
		})
	}
	stopping := func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
	for {
		select {
		case <-ctx.Done():
			return
		case <-stateTicks:
			s.PublishState()
		case <-saveTicks:
			if stopping() {
				continue
			}
			if running {
				pending = true
			} else {
				startSave()
			}
		case <-completed:
			running = false
			if pending {
				pending = false
				if !stopping() && ctx.Err() == nil {
					startSave()
				}
			}
		}
	}
}
