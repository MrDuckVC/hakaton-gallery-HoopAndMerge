// Package runtime coordinates durable room state through consumer-owned ports.
package runtime

import (
	"context"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Catalog provides immutable room content and all protected spawn locations.
type Catalog interface {
	Geometry(string) (world.Room, error)
	Manifest() world.Manifest
	Rooms() []world.Room
	SpawnPositions(string) []player.Vector
	HasGuide(string) bool
	Package(string) (world.RoomPackage, error)
	Spawn(string) (player.State, error)
}

// Store persists an entire overlay atomically. Absent rows represent revision 1.
// CompareAndSwap must insert only for expected=1; a conflict never changes state.
// An uncertain commit returns domain.CommitUnknown, never a speculative success.
type Store interface {
	Read(context.Context, string, string) (domain.Overlay, bool, error)
	CompareAndSwap(context.Context, int64, domain.Overlay) error
}

// RoleStore verifies persisted admin membership for each administrative request.
type RoleStore interface {
	IsAdmin(context.Context, string) (bool, error)
}

// Publisher schedules bounded room notifications, without performing network I/O.
// Invalidation closes affected owning sockets with 1011 after uncertain commits.
type Publisher interface {
	PublishRuntime(domain.Room)
	InvalidateRuntime(string)
}

// Sizer measures the actual public wire payload before persisting it.
type Sizer interface {
	RuntimeBytes(domain.Room) (int, error)
}

// Summary is the administrative room list item.
type Summary struct {
	RoomID, Title string
	Revision      int64
}
