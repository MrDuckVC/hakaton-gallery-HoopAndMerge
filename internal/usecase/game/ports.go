// Package game owns active player identity, ordered presence and command admission.
package game

import (
	"context"
	"time"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
	authcase "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/auth"
)

// Sessions is the persisted authentication boundary used by the connection scenario.
type Sessions interface {
	Authenticate(context.Context, string) (authdomain.Session, authdomain.User, error)
	Me(context.Context, string) (authcase.MeResult, error)
	Observe(context.Context, authdomain.Session) (<-chan authcase.EndReason, func(), error)
}

// Catalog provides immutable room reservations from B03.
type Catalog interface {
	Package(string) (worlddomain.RoomPackage, error)
}

// Identity is prepared before admission without holding the registry lock.
type Identity struct {
	Session authdomain.Session
	User    authdomain.User
	State   player.State
	Package worlddomain.RoomPackage
}

// Player is a transport-independent presence record.
type Player struct {
	User                  authdomain.User
	State                 player.State
	MovementEpoch, AckSeq int64
}

// Snapshot is formed atomically with subsequent presence changes.
type Snapshot struct {
	WorldVersion string
	Self         Player
	Players      []Player
	CurrentRoom  worlddomain.RoomPackage
}

// Failure is a safe application error. Drop indicates an intentionally ignored move.
type Failure struct {
	Code         string
	RetryAfterMS int64
	Drop         bool
}

// Event is an ordered application notification, converted to JSON only by the adapter.
type Event struct {
	Type, RequestID string
	Snapshot        Snapshot
	Player          Player
	Players         []Player
	CurrentRoom     worlddomain.RoomPackage
	MovementEpoch   int64
	UserID          string
	Failure         Failure
}

// Sink performs bounded, nonblocking memory operations only, never network I/O.
// Calls are ordered by Service. Close schedules transport shutdown without waiting.
type Sink interface {
	Enqueue(Event) bool
	Close(int)
}

// Command contains validated input; movement and interaction rules are supplied by B05.
type Command struct {
	Type, RequestID, WorldVersion, DoorID, Text, PhotoID, ExhibitID, GuideID, GenerationID string
	MovementEpoch, Seq                                                                     int64
	Position                                                                               player.Vector
	Rotation                                                                               player.Rotation
}

// Handler supplies game/chat/guide behavior after transport admission.
// Implementations must recheck connection identity when committing asynchronous results.
type Handler interface {
	Handle(context.Context, string, string, Command) Failure
}

// Prepare resolves the real saved pose and current-room package through existing ports.
func Prepare(ctx context.Context, sessions Sessions, catalog Catalog, token string) (Identity, error) {
	session, user, err := sessions.Authenticate(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	me, err := sessions.Me(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	if me.User.ID != user.ID {
		return Identity{}, authdomain.Unauthenticated
	}
	pkg, err := catalog.Package(me.State.RoomID)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Session: session, User: user, State: me.State, Package: pkg}, nil
}

// PendingHandler rejects unavailable subsystems without pretending a command succeeded.
type PendingHandler struct{}

// Handle is replaced by B05 and the chat/guide implementations at composition time.
func (PendingHandler) Handle(_ context.Context, _, _ string, c Command) Failure {
	switch c.Type {
	case "chat.send", "guide.ask", "guide.cancel":
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	default:
		return Failure{Code: "GAME_NOT_READY"}
	}
}

// Clock supplies server time for sliding windows.
type Clock func() time.Time

// World supplies immutable geometry, packages, and directed spawns.
type World interface {
	Catalog
	Geometry(string) (worlddomain.Room, error)
	Spawn(string) (player.State, error)
}

// PositionStore saves a server-accepted pose with optimistic revision checking.
// requireSession is false only for flushing already accepted state after disconnect.
type PositionStore interface {
	PlayerState(context.Context, string) (player.State, error)
	SaveState(context.Context, authdomain.Session, player.State, player.State, bool) (player.State, error)
}

// Questions cancels an unfinished question before a room transition or replacement.
type Questions interface {
	CancelQuestion(context.Context, string, string) error
}

// NoQuestions is used until the generation subsystem is connected.
type NoQuestions struct{}

// CancelQuestion succeeds because no questions can be created in this composition.
func (NoQuestions) CancelQuestion(context.Context, string, string) error { return nil }
