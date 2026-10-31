package guide

import (
	"context"
	"errors"
	"math"
	"sync/atomic"

	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/guide"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

// Service coordinates persisted work and orders events before releasing the scheduler.
// A channel gate serializes short transactions without holding a mutex during I/O.
type Service struct {
	store       Store
	world       World
	sessions    Sessions
	events      Events
	model       Model
	clock       Clock
	newID       func() string
	gate        chan struct{}
	wake        chan struct{}
	dirty       atomic.Bool
	stopping    bool
	runningID   string
	cancelModel context.CancelFunc
}

// New constructs a guide service. Recovery must precede starting listeners.
func New(store Store, world World, sessions Sessions, events Events, model Model, clock Clock, newID func() string) *Service {
	return &Service{store: store, world: world, sessions: sessions, events: events, model: model, clock: clock, newID: newID, gate: make(chan struct{}, 1), wake: make(chan struct{}, 1)}
}
func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-s.gate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) release() { <-s.gate }
func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func failure(err error) Failure {
	var c auth.Code
	if errors.As(err, &c) {
		f := Failure{Code: string(c)}
		if f.Code == "GUIDE_QUEUE_FULL" {
			f.RetryAfterMS = 5000
		}
		return f
	}
	return Failure{Code: "SERVICE_UNAVAILABLE"}
}
func missing(err error) bool { return errors.Is(err, auth.Code("NOT_FOUND")) }

// Recover is called before HTTP admission and after an uncertain storage result.
func (s *Service) Recover(ctx context.Context) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	return s.recoverLocked(ctx)
}
func (s *Service) recoverLocked(ctx context.Context) error {
	if s.cancelModel != nil {
		s.cancelModel()
	}
	pending, err := s.store.Pending(ctx)
	if err != nil {
		s.dirty.Store(true)
		return err
	}
	now := s.clock.Now()
	if err = s.store.Recover(ctx, now); err != nil {
		s.dirty.Store(true)
		for _, g := range pending {
			s.events.CloseGuideConnection(g.UserID, g.ConnectionID, 1011)
		}
		return err
	}
	s.dirty.Store(false)
	for _, g := range pending {
		g.Status = "failed"
		g.Error = domain.Failure("SERVER_RESTARTED")
		g.AssistantMessage = nil
		g.FinishedAt = &now
		s.events.Emit(g.UserID, g.ConnectionID, Event{Type: "guide.failed", RequestID: g.RequestID, Generation: g})
	}
	return nil
}

// Ask checks the durable key before game admission or charging the new-question window.
func (s *Service) Ask(ctx context.Context, in Input, request, guideID, text, exhibitID string, admit Admission) Failure {
	p, err := domain.Normalize(guideID, text, exhibitID)
	if err != nil {
		return failure(err)
	}
	if err = s.acquire(ctx); err != nil {
		return failure(err)
	}
	defer s.release()
	if s.stopping {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	if s.dirty.Load() {
		if err = s.recoverLocked(ctx); err != nil {
			return failure(err)
		}
	}
	old, err := s.store.ByRequest(ctx, in.Session.UserID, request)
	if err == nil {
		if old.Fingerprint != p.Fingerprint() {
			return Failure{Code: "IDEMPOTENCY_CONFLICT"}
		}
		s.events.Emit(in.Session.UserID, in.ConnectionID, Event{Type: "guide.accepted", RequestID: request, Generation: old})
		return Failure{}
	}
	if !missing(err) {
		return failure(err)
	}
	if !in.Ready {
		return Failure{Code: "GAME_NOT_READY"}
	}
	if f := s.validate(in, p); f.Code != "" {
		return f
	}
	if f := admit(); f.Code != "" {
		return f
	}
	now := s.clock.Now()
	g := domain.Generation{ID: s.newID(), UserID: in.Session.UserID, RequestID: request, GuideID: p.GuideID, ExhibitID: p.ExhibitID, Fingerprint: p.Fingerprint(), Status: "queued", ConnectionID: in.ConnectionID, CreatedAt: now, UserMessage: domain.Message{ID: s.newID(), Text: p.Text, CreatedAt: now}}
	g, err = s.store.Create(ctx, in.Session, g)
	if err != nil {
		if errors.Is(err, auth.Unavailable) {
			s.dirty.Store(true)
		}
		return failure(err)
	}
	if !s.events.Emit(in.Session.UserID, in.ConnectionID, Event{Type: "guide.accepted", RequestID: request, Generation: g}) {
		_, _, err = s.finishLocked(ctx, g, "CANCELLED", "")
		if err != nil {
			return failure(err)
		}
	}
	s.notify()
	return Failure{}
}

func (s *Service) validate(in Input, p domain.Payload) Failure {
	room, err := s.world.Geometry(in.State.RoomID)
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	var guide world.Guide
	found := false
	for _, g := range room.Guides {
		if g.ID == p.GuideID {
			guide = g
			found = true
			break
		}
	}
	if !found {
		return Failure{Code: "NOT_FOUND"}
	}
	if p.ExhibitID != "" {
		exists := false
		for _, ex := range room.Exhibits {
			if ex.ID == p.ExhibitID {
				exists = true
				break
			}
		}
		if !exists {
			return Failure{Code: "NOT_FOUND"}
		}
	}
	camera, target := in.State.Position, guide.Position
	camera.Y += 1.65
	target.Y += 1.65
	if math.Hypot(camera.X-target.X, camera.Z-target.Z) > 3 || !world.Visible(room, camera, target) {
		return Failure{Code: "OUT_OF_RANGE"}
	}
	return Failure{}
}

// Cancel sends the original ask failure before acknowledging the cancel command.
func (s *Service) Cancel(ctx context.Context, in Input, request, id string) Failure {
	if err := s.acquire(ctx); err != nil {
		return failure(err)
	}
	defer s.release()
	if s.dirty.Load() {
		if err := s.recoverLocked(ctx); err != nil {
			return failure(err)
		}
	}
	g, err := s.store.Get(ctx, in.Session.UserID, id)
	if err != nil {
		return failure(err)
	}
	if !g.Terminal() {
		g, _, err = s.finishLocked(ctx, g, "CANCELLED", "")
		if err != nil {
			return failure(err)
		}
	}
	s.events.Emit(in.Session.UserID, in.ConnectionID, Event{Type: "guide.cancelled", RequestID: request, Generation: g})
	s.notify()
	return Failure{}
}

// CancelQuestion is connection-scoped; old disconnects cannot cancel new questions.
func (s *Service) CancelQuestion(ctx context.Context, user, connection string) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	if s.dirty.Load() {
		return s.recoverLocked(ctx)
	}
	pending, err := s.store.Pending(ctx)
	if err != nil {
		s.dirty.Store(true)
		return err
	}
	for _, g := range pending {
		if g.UserID == user && g.ConnectionID == connection {
			if _, _, err = s.finishLocked(ctx, g, "CANCELLED", ""); err != nil {
				return err
			}
		}
	}
	s.notify()
	return nil
}

// CancelGenerations is the shutdown port, invoked after game admission stops.
func (s *Service) CancelGenerations(ctx context.Context) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	s.stopping = true
	if s.cancelModel != nil {
		s.cancelModel()
	}
	pending, err := s.store.Pending(ctx)
	if err != nil {
		s.dirty.Store(true)
		return err
	}
	for _, g := range pending {
		if _, _, err = s.finishLocked(ctx, g, "CANCELLED", ""); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finishLocked(ctx context.Context, g domain.Generation, code, text string) (domain.Generation, bool, error) {
	now := s.clock.Now()
	g.FinishedAt = &now
	event := "guide.completed"
	if code != "" {
		g.Status = "failed"
		g.Error = domain.Failure(code)
		g.AssistantMessage = nil
		event = "guide.failed"
	} else {
		g.Status = "completed"
		g.Error = nil
		g.AssistantMessage = &domain.Message{ID: s.newID(), Text: text, CreatedAt: now}
	}
	current, won, err := s.store.Finish(ctx, g)
	if err != nil {
		s.dirty.Store(true)
		if s.cancelModel != nil {
			s.cancelModel()
		}
		s.events.CloseGuideConnection(g.UserID, g.ConnectionID, 1011)
		return current, false, err
	}
	if won {
		if g.ID == s.runningID && code != "" && s.cancelModel != nil {
			s.cancelModel()
		}
		s.events.Emit(g.UserID, g.ConnectionID, Event{Type: event, RequestID: g.RequestID, Generation: current})
	}
	return current, won, nil
}
