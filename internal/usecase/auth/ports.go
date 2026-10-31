// Package auth orchestrates accounts, fixed-lifetime sessions and safe pose restoration.
package auth

import (
	"context"
	"time"

	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
)

// Store implements atomic writes required by authentication, never exposing SQL types.
type Store interface {
	FindAccount(context.Context, string) (domain.Account, error)
	// CreateLogin atomically inserts an optional new account/state, creates the session,
	// and revokes the valid previous cookie session. It returns that session's ID.
	CreateLogin(context.Context, *Registration, domain.Session, *[32]byte, time.Time) (string, error)
	FindSession(context.Context, [32]byte) (domain.Session, domain.User, error)
	SessionByID(context.Context, string) (domain.Session, error)
	Revoke(context.Context, [32]byte, time.Time) (string, error)
	PlayerState(context.Context, string) (player.State, error)
	// RepairState writes only under a still-valid locked session and matching pose revision.
	RepairState(context.Context, domain.Session, player.State, player.State) (bool, error)
}

// Registration is an account and its verified initial pose, committed together.
type Registration struct {
	Account domain.Account
	State   player.State
}

// Security is the cryptographic boundary; password and token material must not be logged.
type Security interface {
	HashPassword(context.Context, string) (string, error)
	VerifyPassword(context.Context, string, string) (bool, error)
	NewToken() (string, [32]byte, error)
	TokenHash(string) [32]byte
	NewID() string
	AvatarColor() string
}

// World supplies validated positions from the current immutable catalog.
type World interface {
	Start(context.Context) (player.State, error)
	Valid(context.Context, player.State) (bool, error)
}

// EndReason is independent of WebSocket close codes.
type EndReason string

const (
	Revoked EndReason = "revoked"
	Expired EndReason = "expired"
)

// Events observes a session until expiry, revocation, or explicit cancellation.
// Observe must register synchronously; the caller rechecks persisted validity afterward.
type Events interface {
	Observe(domain.Session) (<-chan EndReason, func())
	Revoke(string)
}
