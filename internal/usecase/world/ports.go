// Package world validates immutable content and authorizes current-room downloads.
package world

import (
	"context"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// File holds the exact bytes of one validated local regular file.
type File struct {
	Identity, ContentType string
	Bytes                 []byte
}

// LoadedWorld separates semantic content from original wire bytes.
type LoadedWorld struct {
	Profiles      map[string]guide.Profile
	Input         domain.Input
	Rooms, Assets map[string]File
}

// Source loads a consistent version without exposing filesystem APIs.
type Source interface {
	Load(context.Context) (LoadedWorld, error)
}

// SnapshotSizer measures an upper bound using the actual wire representation.
type SnapshotSizer interface {
	MaxSnapshotBytes(domain.RoomPackage) (int64, error)
}

// Sessions verifies current persisted authentication without repairing a player's pose.
type Sessions interface {
	Authenticate(context.Context, string) (auth.Session, auth.User, error)
}

// Ownership atomically checks active WS ownership and snapshots its confirmed room.
// Implementations must check both session and user, including during loading before ready.
// B04 supplies the game implementation; a stored SQL pose alone never authorizes access.
type Ownership interface {
	OwningRoom(context.Context, string, string) (string, error)
}

// NoConnections refuses downloads until a game subsystem provides active ownership.
type NoConnections struct{}

// OwningRoom never represents an authenticated login as an active game connection.
func (NoConnections) OwningRoom(context.Context, string, string) (string, error) {
	return "", domain.NotReady
}
