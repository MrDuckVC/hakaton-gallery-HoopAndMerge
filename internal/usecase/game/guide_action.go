package game

import (
	"context"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// GuideActions is the consumer-owned port for durable exhibition changes.
type GuideActions interface {
	Action(context.Context, string, player.Vector, domain.GuideAction, func() error, func(domain.Room, domain.GuideEffect)) error
}

// ConfigureGuideActions installs actions separately from the question queue.
func (s *Service) ConfigureGuideActions(actions GuideActions) { s.guideActions = actions }

func (s *Service) guideAction(ctx context.Context, userID, id string, cmd Command) Failure {
	s.mu.Lock()
	c := s.active[userID]
	if c == nil || c.id != id || c.ended || s.stopping {
		s.mu.Unlock()
		return Failure{Drop: true}
	}
	position, roomID := c.identity.State.Position, c.identity.State.RoomID
	s.mu.Unlock()
	guard := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.active[userID] != c || c.ended || s.stopping {
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
	if s.guideActions == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	a := domain.GuideAction{GuideID: cmd.GuideID, Text: cmd.Text, ExpectedRevision: cmd.ExpectedRevision}
	err := s.guideActions.Action(ctx, roomID, position, a, guard, func(room domain.Room, effect domain.GuideEffect) {
		s.mu.Lock()
		defer s.mu.Unlock()
		snapshot, ok := s.npcSnapshotLocked(room)
		if !ok {
			return
		}
		for _, other := range s.active {
			if other.ended || other.identity.State.RoomID != roomID || other.identity.State.WorldVersion != room.WorldVersion {
				continue
			}
			if other == c {
				other.sink.Enqueue(Event{Type: "guide.action.completed", RequestID: cmd.RequestID, GuideID: cmd.GuideID, GuideEffect: effect, Runtime: room})
			} else {
				other.sink.Enqueue(Event{Type: "world.runtime", Runtime: room})
			}
			s.enqueueNPCLocked(other.sink, snapshot, "")
		}
		s.pruneNPCLocked()
	})
	return objectFailure(err)
}
