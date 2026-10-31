// Package photo orchestrates idempotent private uploads through infrastructure ports.
package photo

import (
	"context"
	"errors"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
)

// ErrUncertain means COMMIT may have succeeded; files must survive until reconciliation.
//
//nolint:gochecknoglobals // Sentinel identity is part of the repository port contract.
var ErrUncertain = errors.New("photo commit outcome unknown")

// Sessions supplies persisted identity without restoring or changing a player's pose.
type Sessions interface {
	Authenticate(context.Context, string) (auth.Session, auth.User, error)
}

// Images validates original pixels and constructs a bounded JPEG thumbnail.
type Images interface {
	Prepare(context.Context, []byte) (Prepared, error)
}

// Prepared carries only validated image metadata and derived bytes.
type Prepared struct {
	ContentType   string
	Width, Height int
	Thumbnail     []byte
}

// Files publishes a pair atomically under a fresh server-generated ID.
// Save cleans its own failed attempt. Delete must never touch another ID.
type Files interface {
	Save(context.Context, string, []byte, []byte) error
	Delete(context.Context, string) error
}

// Game keeps accepted state stable while committing, without a mutex on I/O.
// Capture does not enforce readiness: replay remains available without a WS.
type Game interface {
	Capture(auth.Session) string
	WithCapture(context.Context, auth.Session, string, domain.Request, func(string, func() error) error) error
}

// Repository serializes one user's key across service instances without a SQL transaction on file I/O.
type Repository interface {
	WithKey(context.Context, string, string, func(LockedStore) error) error
}

// LockedStore is scoped to a key. Create revalidates the session and calls check immediately before COMMIT.
// A missing Find returns NOT_FOUND. Create returns ErrUncertain if rollback cannot be established.
type LockedStore interface {
	Find(context.Context) (domain.Photo, error)
	Create(context.Context, auth.Session, domain.Photo, func() error) (domain.Photo, error)
}
