package world

import (
	"context"

	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

// Service authorizes immutable downloads using current persisted sessions and WS ownership.
type Service struct {
	catalog   *Catalog
	sessions  Sessions
	ownership Ownership
}

// New connects application ports; ownership must be provided even before B04 is available.
func New(c *Catalog, s Sessions, o Ownership) *Service {
	return &Service{catalog: c, sessions: s, ownership: o}
}

// Manifest is available to every currently authenticated session, without a game connection.
func (s *Service) Manifest(ctx context.Context, token string) (domain.Manifest, error) {
	if _, _, err := s.sessions.Authenticate(ctx, token); err != nil {
		return domain.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Manifest{}, err
	}
	return s.catalog.Manifest(), nil
}
func (s *Service) authorize(ctx context.Context, token, version string) (string, error) {
	session, user, err := s.sessions.Authenticate(ctx, token)
	if err != nil {
		return "", err
	}
	if !domain.ValidID(version) {
		return "", auth.Validation
	}
	if version != s.catalog.manifest.Version {
		return "", domain.VersionMismatch
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	room, err := s.ownership.OwningRoom(ctx, session.ID, user.ID)
	if err != nil {
		return "", err
	}
	if _, ok := s.catalog.rooms[room]; !ok {
		return "", domain.NotReady
	}
	return room, nil
}

// Room checks ownership once, permitting an already authorized response to finish after transition.
func (s *Service) Room(ctx context.Context, token, version, id string) (File, error) {
	room, err := s.authorize(ctx, token, version)
	if err != nil {
		return File{}, err
	}
	if !domain.ValidID(id) {
		return File{}, auth.Validation
	}
	if _, ok := s.catalog.rooms[id]; !ok {
		return File{}, domain.NotFound
	}
	if id != room {
		return File{}, domain.RoomNotAccessible
	}
	return s.catalog.Descriptor(id)
}

// Asset permits only dependencies of the owning connection's confirmed room, including before ready.
func (s *Service) Asset(ctx context.Context, token, version, id string) (File, error) {
	room, err := s.authorize(ctx, token, version)
	if err != nil {
		return File{}, err
	}
	if !domain.ValidID(id) {
		return File{}, auth.Validation
	}
	if _, ok := s.catalog.assets[id]; !ok {
		return File{}, domain.NotFound
	}
	for _, a := range s.catalog.packages[room].Assets {
		if a.ID == id {
			return s.catalog.Asset(id)
		}
	}
	return File{}, domain.RoomNotAccessible
}
