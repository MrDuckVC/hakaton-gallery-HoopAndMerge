// Package guide implements durable generation admission, execution and recovery.
package guide

import (
	"context"
	"time"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Store owns atomic reservations and compare-and-set terminal transitions.
type Store interface {
	ByRequest(context.Context, string, string) (domain.Generation, error)
	Create(context.Context, auth.Session, domain.Generation) (domain.Generation, error)
	Claim(context.Context, time.Time) (domain.Generation, error)
	Pending(context.Context) ([]domain.Generation, error)
	Finish(context.Context, domain.Generation) (domain.Generation, bool, error)
	Get(context.Context, string, string) (domain.Generation, error)
	History(context.Context, string, string, int, string) ([]domain.Generation, string, error)
	Recover(context.Context, time.Time) error
}

// Model emits added text synchronously and returns promptly on context cancellation.
// Implementations must not invoke emit after returning.
type Model interface {
	Stream(context.Context, ModelRequest, func(string) error) error
}

// World provides immutable geometry and known personalities for private history.
type World interface {
	Geometry(string) (world.Room, error)
	HasGuide(string) bool
}

// Sessions authenticates HTTP history without requiring an active game connection.
type Sessions interface {
	Authenticate(context.Context, string) (auth.Session, auth.User, error)
}

// Events queues bounded notifications only for the named live connection.
// Implementations never call back into the guide service or acquire a user operation gate.
type Events interface {
	Emit(string, string, Event) bool
	CloseGuideConnection(string, string, int)
}

// Event is transport independent. Index/Text are used only for delta.
type Event struct {
	Type, RequestID, GenerationID, Text string
	Index                               int
	Generation                          domain.Generation
}

// Input is a server-owned snapshot under the game user's operation gate.
type Input struct {
	Session      auth.Session
	State        player.State
	ConnectionID string
	Ready        bool
}

// Failure is a pre-acceptance command error.
type Failure struct {
	Code         string
	RetryAfterMS int64
}

// Admission charges only a question whose durable key does not exist.
type Admission func() Failure

// Clock permits deterministic queue and execution timeout tests.
type Clock struct {
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
}
