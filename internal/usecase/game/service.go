package game

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

type connection struct {
	id         string
	identity   Identity
	sink       Sink
	seen       map[string]struct{}
	ended      bool
	reloadPose bool
	ready      bool
	epoch, ack int64
	lastMove   time.Time
}
type reservation struct{ identity Identity }

// Service serializes registry and presence changes; its lock never covers external I/O.
type Service struct {
	mu           sync.Mutex
	now          Clock
	next         uint64
	stopping     bool
	active       map[string]*connection
	pending      map[string]reservation
	limits       map[string]*windows
	gates        map[string]chan struct{}
	world        World
	store        PositionStore
	questions    Questions
	inspections  InspectionStore
	photos       PhotoPublisher
	newMessageID func() string
}

// New constructs a registry limited to 32 active or reserved accounts.
func New(now Clock) *Service {
	return &Service{now: now, active: make(map[string]*connection), pending: make(map[string]reservation), limits: make(map[string]*windows), gates: make(map[string]chan struct{})}
}

// Reserve admits before upgrade, leaving an existing connection untouched until Attach.
func (s *Service) Reserve(identity Identity) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return "", authdomain.Unavailable
	}
	users := make(map[string]bool, len(s.active)+len(s.pending))
	for id := range s.active {
		users[id] = true
	}
	for _, r := range s.pending {
		users[r.identity.User.ID] = true
	}
	if !users[identity.User.ID] && len(users) >= 32 {
		return "", authdomain.Unavailable
	}
	// Bound simultaneous handshakes as well as active users; one pending per account.
	for _, r := range s.pending {
		if r.identity.User.ID == identity.User.ID {
			return "", authdomain.Unavailable
		}
	}
	s.next++
	ticket := strconv.FormatUint(s.next, 10)
	s.pending[ticket] = reservation{identity: identity}
	return ticket, nil
}

// Release abandons only this handshake's reservation.
func (s *Service) Release(ticket string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.pending, ticket) }

// Attach publishes the new owner only after upgrade; snapshot is always queued first.
func (s *Service) Attach(ticket string, sink Sink) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.AttachContext(ctx, ticket, sink)
}

// AttachContext serializes replacement with all accepted state writes for this user.
func (s *Service) AttachContext(ctx context.Context, ticket string, sink Sink) error {
	s.mu.Lock()
	r, ok := s.pending[ticket]
	s.mu.Unlock()
	if !ok {
		return authdomain.Unavailable
	}
	release, err := s.acquire(ctx, r.identity.User.ID)
	if err != nil {
		return err
	}
	defer release()
	if s.store != nil {
		s.mu.Lock()
		old := s.active[r.identity.User.ID]
		reload := old == nil || old.reloadPose
		if old != nil {
			r.identity.State = old.identity.State
		}
		s.mu.Unlock()
		if reload {
			state, readErr := s.store.PlayerState(ctx, r.identity.User.ID)
			if readErr != nil {
				return readErr
			}
			// Prepare has already durably repaired the pose through auth.Me.
			r.identity.State = state
		}
		pkg, packageErr := s.world.Package(r.identity.State.RoomID)
		if packageErr != nil {
			return packageErr
		}
		r.identity.Package = pkg
		if old != nil {
			if err = s.questions.CancelQuestion(ctx, r.identity.User.ID, old.id); err != nil {
				return err
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return s.attachPrepared(ticket, sink, r)
}
func (s *Service) attachPrepared(ticket string, sink Sink, r reservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[ticket]
	delete(s.pending, ticket)
	if !ok || s.stopping {
		return authdomain.Unavailable
	}
	userID := r.identity.User.ID
	snapshot := Snapshot{WorldVersion: r.identity.State.WorldVersion, Self: presence(r.identity), CurrentRoom: r.identity.Package, Players: make([]Player, 0, len(s.active))}
	for id, c := range s.active {
		if id != userID && !c.ended {
			snapshot.Players = append(snapshot.Players, presenceConnection(c))
		}
	}
	if !sink.Enqueue(Event{Type: "world.snapshot", Snapshot: snapshot}) {
		return authdomain.Unavailable
	}
	if old := s.active[userID]; old != nil {
		old.sink.Close(4009)
		s.broadcastLocked(Event{Type: "player.left", UserID: userID}, userID)
	}
	s.active[userID] = &connection{id: ticket, identity: r.identity, sink: sink, seen: make(map[string]struct{}), epoch: 1}
	s.broadcastLocked(Event{Type: "player.joined", Player: presence(r.identity)}, userID)
	return nil
}
func presence(i Identity) Player { return Player{User: i.User, State: i.State, MovementEpoch: 1} }
func presenceConnection(c *connection) Player {
	return Player{User: c.identity.User, State: c.identity.State, MovementEpoch: c.epoch, AckSeq: c.ack}
}
func (s *Service) broadcastLocked(e Event, except string) {
	for id, c := range s.active {
		if id != except {
			c.sink.Enqueue(e)
		}
	}
}

// Detach ignores every late cleanup belonging to a replaced connection.
func (s *Service) Detach(userID, id string) {
	// Revoke admission and HTTP ownership before waiting for this user's gate or SQL.
	// Keep the accepted pose until its serialized final flush has completed.
	s.mu.Lock()
	c := s.active[userID]
	if c == nil || c.id != id {
		s.mu.Unlock()
		return
	}
	c.ended = true
	c.ready = false
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := s.acquire(ctx, userID)
	if err != nil {
		s.remove(userID, id)
		return
	}
	defer release()
	_ = s.flush(ctx, userID, id)
	s.remove(userID, id)
}
func (s *Service) remove(userID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.active[userID]; c != nil && c.id == id {
		delete(s.active, userID)
		s.broadcastLocked(Event{Type: "player.left", UserID: userID}, userID)
	}
}

// Current guards reader and asynchronous session callbacks against replacement.
func (s *Service) Current(userID, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	return c != nil && c.id == id && !c.ended && !s.stopping
}

// Send delivers only to the still-current connection in the registry's event order.
func (s *Service) Send(userID, id string, e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.active[userID]; c != nil && c.id == id {
		c.sink.Enqueue(e)
	}
}

// End terminates only the named connection, never its replacement.
func (s *Service) End(userID, id string, code int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := s.acquire(ctx, userID)
	if err != nil {
		s.closeCurrent(userID, id, code)
		return
	}
	defer release()
	s.closeCurrent(userID, id, code)
}
func (s *Service) closeCurrent(userID, id string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.active[userID]; c != nil && c.id == id {
		c.ended = true
		if code == 1011 {
			c.reloadPose = true
		}
		c.sink.Close(code)
	}
}

// OwningRoom implements the HTTP catalog's existing ownership port, including before ready.
func (s *Service) OwningRoom(ctx context.Context, sessionID, userID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	if c == nil || c.ended || c.identity.Session.ID != sessionID || s.stopping {
		return "", worlddomain.NotReady
	}
	return c.identity.State.RoomID, nil
}

// StopAccepting prevents reservations and new commands during shutdown.
func (s *Service) StopAccepting(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopping = true
	return nil
}

// CloseConnections schedules server-restart closure; the transport waits for its workers.
func (s *Service) CloseConnections(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.active {
		c.ended = true
		c.sink.Close(1012)
	}
	return nil
}

// Check applies connection-local deduplication and user-scoped sliding windows.
func (s *Service) Check(userID, id string, cmd Command) Failure {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	if c == nil || c.ended || c.id != id || s.stopping {
		return Failure{Drop: true}
	}

	now := s.now()
	// Expired inactive records are discarded without resetting any live window.
	for key, w := range s.limits {
		if now.Sub(w.last) >= time.Minute {
			delete(s.limits, key)
		}
	}
	w := s.limits[userID]
	if w == nil {
		w = &windows{moveSecond: -1}
		s.limits[userID] = w
	}
	w.last = now
	if cmd.Type == "player.move" {
		if wait := allow(&w.moves, now, time.Second, 20); wait > 0 {
			second := now.Unix()
			if second != w.moveSecond {
				if second == w.moveSecond+1 {
					w.violatingSeconds++
				} else {
					w.violatingSeconds = 1
				}
				w.moveSecond = second
			}
			if w.violatingSeconds >= 5 {
				c.ended = true
				c.sink.Close(1008)
			}
			return Failure{Drop: true}
		}
		return Failure{}
	}
	// Durable guide replay classification belongs to the guide handler, before charging
	// the new-ask window. AdmitNewAsk provides that hook without treating a replay as new.
	duplicate := false
	if cmd.Type != "guide.ask" && cmd.RequestID != "" {
		key := strings.ToLower(cmd.RequestID)
		_, duplicate = c.seen[key]
		c.seen[key] = struct{}{}
		if len(c.seen) >= 10000 {
			c.ended = true
			c.sink.Close(1012)
		}
	}
	if wait := allow(&w.commands, now, 10*time.Second, 30); wait > 0 {
		return rateFailure(wait)
	}
	if duplicate {
		return Failure{Code: "DUPLICATE_REQUEST"}
	}
	if cmd.Type == "chat.send" {
		if wait := allow(&w.chat, now, 10*time.Second, 5); wait > 0 {
			return rateFailure(wait)
		}
	}

	return Failure{}
}

// AdmitNewAsk is called after the durable idempotency lookup identifies a new question.
func (s *Service) AdmitNewAsk(userID, id string) Failure {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	if c == nil || c.ended || c.id != id || s.stopping {
		return Failure{Drop: true}
	}
	now := s.now()
	w := s.limits[userID]
	if w == nil {
		w = &windows{moveSecond: -1}
		s.limits[userID] = w
	}
	w.last = now
	if wait := allow(&w.ask, now, time.Minute, 3); wait > 0 {
		return rateFailure(wait)
	}
	return Failure{}
}
