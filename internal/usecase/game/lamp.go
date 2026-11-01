package game

import (
	"context"
	"errors"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Objects is the consumer-owned boundary for durable interactions and cleanup.
type Objects interface {
	Act(context.Context, string, domain.ObjectActor, domain.ObjectAction, func() error, func(domain.Room)) error
	ReleaseLamp(context.Context, string, domain.Holder) error
	ReconcileLamp(context.Context, string, func(string, string) bool) error
}

type lampRetry struct {
	due        time.Time
	attempts   int
	generation uint64
}

// ConfigureObjects installs lamp interactions before admission begins.
func (s *Service) ConfigureObjects(objects Objects) {
	s.objects = objects
	s.dirtyLamps = make(map[string]lampRetry)
}

func (s *Service) lampLive(roomID, userID, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[userID]
	return c != nil && c.id == id && !c.ended && !s.stopping && c.identity.State.RoomID == roomID
}

func (s *Service) objectAct(ctx context.Context, userID, id string, cmd Command) Failure {
	if s.objects == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	s.mu.Lock()
	c := s.active[userID]
	if c == nil || c.id != id || c.ended || s.stopping {
		s.mu.Unlock()
		return Failure{Drop: true}
	}
	actor := domain.ObjectActor{Holder: domain.Holder{UserID: userID, ConnectionID: id}, Position: c.identity.State.Position}
	roomID := c.identity.State.RoomID
	s.mu.Unlock()
	guard := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.active[userID]
		if current != c || c.ended || s.stopping {
			return auth.Unauthenticated
		}
		if !c.ready {
			return world.NotReady
		}
		if cmd.MovementEpoch != c.epoch {
			return world.Code("STALE_MOVEMENT_EPOCH")
		}
		return nil
	}
	if err := guard(); err != nil {
		return objectFailure(err)
	}
	if cmd.Object.Action == "take" {
		if err := s.objects.ReconcileLamp(ctx, roomID, func(u, c string) bool { return s.lampLive(roomID, u, c) }); err != nil {
			return objectFailure(err)
		}
	}
	err := s.objects.Act(ctx, roomID, actor, cmd.Object, guard, func(room domain.Room) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.active[userID] == c && !c.ended && !s.stopping {
			c.sink.Enqueue(Event{Type: "object.acted", RequestID: cmd.RequestID, ObjectAction: cmd.Object.Action, Runtime: room})
		} else {
			s.markLampDirtyLocked(roomID)
		}
		for user, other := range s.active {
			if user != userID && !other.ended && other.identity.State.RoomID == roomID && other.identity.State.WorldVersion == room.WorldVersion {
				other.sink.Enqueue(Event{Type: "world.runtime", Runtime: room})
			}
		}
	})
	return objectFailure(err)
}

func objectFailure(err error) Failure {
	if err == nil {
		return Failure{}
	}
	if errors.Is(err, auth.Unauthenticated) || errors.Is(err, context.Canceled) {
		return Failure{Drop: true}
	}
	var code domain.Code
	if errors.As(err, &code) && code != domain.CommitUnknown {
		return Failure{Code: string(code)}
	}
	var wc world.Code
	if errors.As(err, &wc) {
		return Failure{Code: string(wc)}
	}
	if errors.Is(err, auth.Validation) {
		return Failure{Code: string(auth.Validation)}
	}
	return Failure{Code: "SERVICE_UNAVAILABLE"}
}

// markLampDirtyLocked coalesces by room, retaining work even during reconnect churn.
func (s *Service) markLampDirtyLocked(roomID string) {
	if s.objects == nil {
		return
	}
	item, ok := s.dirtyLamps[roomID]
	if !ok {
		item.due = s.now().Add(time.Second)
	}
	s.lampSequence++
	item.generation = s.lampSequence
	s.dirtyLamps[roomID] = item
}

// releaseConnectionLamp is called under the user's operation gate, outside mu.
func (s *Service) releaseConnectionLamp(ctx context.Context, roomID, userID, id string) error {
	if s.objects == nil {
		return nil
	}
	err := s.objects.ReleaseLamp(ctx, roomID, domain.Holder{UserID: userID, ConnectionID: id})
	if err != nil {
		s.closeCurrent(userID, id, 1011)
		s.mu.Lock()
		s.markLampDirtyLocked(roomID)
		s.mu.Unlock()
	}
	return err
}

// RunLampCleanup owns one retry worker; cancellation stops it before storage closes.
func (s *Service) RunLampCleanup(ctx context.Context) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.retryLamps(ctx, s.now())
		}
	}
}

func (s *Service) retryLamps(ctx context.Context, now time.Time) {
	s.mu.Lock()
	due := make(map[string]lampRetry)
	for room, item := range s.dirtyLamps {
		if !now.Before(item.due) {
			due[room] = item
		}
	}
	s.mu.Unlock()
	for room, item := range due {
		if ctx.Err() != nil {
			return
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.objects.ReconcileLamp(attemptCtx, room, func(u, c string) bool { return s.lampLive(room, u, c) })
		cancel()
		s.mu.Lock()
		current, exists := s.dirtyLamps[room]
		if exists {
			if err == nil && current.generation == item.generation {
				delete(s.dirtyLamps, room)
			} else if err != nil {
				current.attempts++
				delay := 5 * time.Second
				if current.attempts == 1 {
					delay = 2 * time.Second
				}
				current.due = s.now().Add(delay)
				s.dirtyLamps[room] = current
			}
		}
		s.mu.Unlock()
	}
}

// ReleaseObjects runs after stopping admission and before closing connections/storage.
func (s *Service) ReleaseObjects(ctx context.Context) error {
	if s.objects == nil {
		return nil
	}
	s.mu.Lock()
	rooms := make(map[string]bool)
	for _, c := range s.active {
		rooms[c.identity.State.RoomID] = true
	}
	for room := range s.dirtyLamps {
		rooms[room] = true
	}
	s.mu.Unlock()
	var result error
	for room := range rooms {
		if err := s.objects.ReconcileLamp(ctx, room, func(string, string) bool { return false }); err != nil {
			result = errors.Join(result, err)
			s.InvalidateRuntime(room)
		}
	}
	return result
}
