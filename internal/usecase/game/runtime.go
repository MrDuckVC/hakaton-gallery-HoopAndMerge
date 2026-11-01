package game

import (
	"context"

	runtime "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

// Runtime serializes durable snapshots with room mutations, outside the game mutex.
// The callback may only enqueue bounded notifications and change in-memory state.
type Runtime interface {
	WithRoom(context.Context, string, func(runtime.Room) error) error
}

// RoomSnapshots is the optional room simulation boundary. S05 supplies NPC state.
// EnqueueSnapshot performs bounded memory operations only and queues a complete
// snapshot with the supplied request ID through the existing ordered sink.
type RoomSnapshots interface {
	EnqueueSnapshot(roomID, requestID string, sink Sink) bool
}

// ConfigureRoomSnapshots connects simulation snapshots before serving.
func (s *Service) ConfigureRoomSnapshots(snapshots RoomSnapshots) { s.roomSnapshots = snapshots }

func (s *Service) enqueueRuntimeLocked(sink Sink, room runtime.Room, requestID string) bool {
	if !sink.Enqueue(Event{Type: "world.runtime", RequestID: requestID, Runtime: room}) {
		return false
	}
	return s.roomSnapshots == nil || s.roomSnapshots.EnqueueSnapshot(room.RoomID, requestID, sink)
}

// ConfigureRuntime installs room snapshots before accepting connections.
func (s *Service) ConfigureRuntime(source Runtime) { s.runtime = source }

// PublishRuntime queues committed state to all current room owners, including not-ready.
// Called while the runtime usecase owns the room gate, never a database transaction.
func (s *Service) PublishRuntime(room runtime.Room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.active {
		if !c.ended && c.identity.State.RoomID == room.RoomID && c.identity.State.WorldVersion == room.WorldVersion {
			s.enqueueRuntimeLocked(c.sink, room, "")
		}
	}
}

// InvalidateRuntime immediately revokes room ownership after an uncertain COMMIT.
// A reconnect must read canonical state before attaching; no RAM rollback is allowed.
func (s *Service) InvalidateRuntime(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.active {
		if !c.ended && c.identity.State.RoomID == roomID {
			c.ended = true
			c.reloadPose = true
			c.ready = false
			c.sink.Close(1011)
		}
	}
}

func (s *Service) syncRoom(ctx context.Context, userID, id, roomID, requestID string) Failure {
	if s.runtime == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	err := s.runtime.WithRoom(ctx, roomID, func(room runtime.Room) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		c := s.active[userID]
		if c != nil && c.id == id && !c.ended && !s.stopping && c.identity.State.RoomID == roomID {
			s.enqueueRuntimeLocked(c.sink, room, requestID)
		}
		return nil
	})
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	return Failure{}
}
