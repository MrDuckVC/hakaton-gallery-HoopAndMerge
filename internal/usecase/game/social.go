package game

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	authdomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// PublishedPhoto is immutable public metadata returned after durable publication.
// The port owns authorization and must never return a private photo on success.
type PublishedPhoto struct {
	ID, OwnerID, Caption, RoomID string
	ExhibitID                    *string
	Published                    bool
	ImageURL                     string
	ImageBytes                   int64
	ThumbnailURL                 string
	ThumbnailBytes               int64
	Width, Height                int
	CreatedAt                    time.Time
}

// ChatMessage is transient; no shared chat history is persisted.
type ChatMessage struct {
	ID, AuthorID, DisplayName, Text string
	Photo                           *PublishedPhoto
}

// InspectionStore atomically updates the inspection and its single memory event.
// The adapter revalidates the session within the write transaction.
type InspectionStore interface {
	RecordInspection(context.Context, authdomain.Session, player.State, string) (time.Time, error)
}

// PhotoPublisher authorizes the owner and commits publication before returning.
// B10 implements this port. All failures prevent the entire chat broadcast.
type PhotoPublisher interface {
	PublishPhoto(context.Context, authdomain.Session, string) (PublishedPhoto, error)
}

// UnavailablePhotos explicitly disables publication until B10 is connected.
type UnavailablePhotos struct{}

// PublishPhoto cannot publish before photo storage is implemented.
func (UnavailablePhotos) PublishPhoto(context.Context, authdomain.Session, string) (PublishedPhoto, error) {
	return PublishedPhoto{}, authdomain.Unavailable
}

// MemoryEvent is private educational context; CreatedAt is the last interaction time.
type MemoryEvent struct {
	ID, UserID, Type, WorldVersion, RoomID string
	ExhibitID, PhotoID                     *string
	CreatedAt                              time.Time
}

// MemoryReader supplies bounded personal events for future guide usecases.
// userID must come from authenticated server identity, never client input.
type MemoryReader interface {
	ListMemoryEvents(context.Context, string, int) ([]MemoryEvent, error)
}

// ConfigureSocial installs B06 ports before admitting connections.
func (s *Service) ConfigureSocial(inspections InspectionStore, photos PhotoPublisher, newID func() string) {
	s.inspections, s.photos, s.newMessageID = inspections, photos, newID
}

// chat runs under the user's operation gate, without the registry mutex on I/O.
func (s *Service) chat(ctx context.Context, c *connection, cmd Command) Failure {
	text := strings.TrimSpace(cmd.Text)
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 500 || (text == "" && cmd.PhotoID == "") || (cmd.PhotoID != "" && !worlddomain.ValidID(cmd.PhotoID)) {
		return Failure{Code: "VALIDATION_ERROR"}
	}
	if s.newMessageID == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	message := ChatMessage{ID: s.newMessageID(), AuthorID: c.identity.User.ID, DisplayName: c.identity.User.DisplayName, Text: text}
	if cmd.PhotoID != "" {
		if s.photos == nil {
			return Failure{Code: "SERVICE_UNAVAILABLE"}
		}
		photo, err := s.photos.PublishPhoto(ctx, c.identity.Session, cmd.PhotoID)
		if err != nil {
			return s.socialFailure(c, err)
		}
		message.Photo = &photo
	}
	if ctx.Err() != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[c.identity.User.ID] != c || c.ended || s.stopping {
		return Failure{Drop: true}
	}
	if text != "" {
		s.npcChatLocked(c)
	}
	for userID, other := range s.active {
		if other.ended {
			continue
		}
		event := Event{Type: "chat.message", Chat: message}
		if userID == c.identity.User.ID {
			event.RequestID = cmd.RequestID
		}
		other.sink.Enqueue(event)
	}
	return Failure{}
}

func (s *Service) inspect(ctx context.Context, c *connection, state player.State, cmd Command) Failure {
	if s.world == nil || s.inspections == nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	room, err := s.world.Geometry(state.RoomID)
	if err != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	var exhibit worlddomain.Exhibit
	found := false
	for _, ex := range room.Exhibits {
		if ex.ID == cmd.ExhibitID {
			exhibit = ex
			found = true
			break
		}
	}
	if !found {
		return Failure{Code: "NOT_FOUND"}
	}
	camera := state.Position
	camera.Y += 1.65
	if math.Hypot(state.Position.X-exhibit.Position.X, state.Position.Z-exhibit.Position.Z) > 3 || !worlddomain.Visible(room, camera, exhibit.Position) {
		return Failure{Code: "OUT_OF_RANGE"}
	}
	saved, err := s.inspections.RecordInspection(ctx, c.identity.Session, state, exhibit.ID)
	if err != nil {
		return s.socialFailure(c, err)
	}
	if ctx.Err() != nil {
		return Failure{Code: "SERVICE_UNAVAILABLE"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[c.identity.User.ID] != c || c.ended || s.stopping {
		return Failure{Drop: true}
	}
	c.sink.Enqueue(Event{Type: "exhibit.inspected", RequestID: cmd.RequestID, ExhibitID: exhibit.ID, InspectedAt: saved})
	return Failure{}
}

func (s *Service) socialFailure(c *connection, err error) Failure {
	if errors.Is(err, authdomain.Unauthenticated) {
		s.closeCurrent(c.identity.User.ID, c.id, 4001)
		return Failure{Drop: true}
	}
	var code authdomain.Code
	if errors.As(err, &code) {
		switch string(code) {
		case "NOT_FOUND", "FORBIDDEN", "VALIDATION_ERROR":
			return Failure{Code: string(code)}
		}
	}
	return Failure{Code: "SERVICE_UNAVAILABLE"}
}
