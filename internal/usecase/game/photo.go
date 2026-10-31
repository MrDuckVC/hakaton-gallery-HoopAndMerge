package game

import (
	"context"
	"math"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/photo"
	worlddomain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
)

// Capture records the current socket without enforcing ready, so replay needs no active socket.
func (s *Service) Capture(session auth.Session) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[session.UserID]
	if c == nil || c.ended || s.stopping || c.identity.Session.ID != session.ID {
		return ""
	}
	return c.id
}

// WithCapture serializes finalization with movement, transitions and socket replacement.
// Its final check also observes immediate detach/shutdown revocation outside the gate.
func (s *Service) WithCapture(ctx context.Context, session auth.Session, id string, r photo.Request, commit func(string, func() error) error) error {
	release, err := s.acquire(ctx, session.UserID)
	if err != nil {
		return err
	}
	defer release()
	check := func() error { _, e := s.photoState(ctx, session, id, r); return e }
	version, err := s.photoState(ctx, session, id, r)
	if err != nil {
		return err
	}
	return commit(version, check)
}
func (s *Service) photoState(ctx context.Context, session auth.Session, id string, r photo.Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active[session.UserID]
	if id == "" || c == nil || c.id != id || c.ended || s.stopping || c.identity.Session.ID != session.ID || !c.ready || c.epoch != r.Epoch || c.identity.State.RoomID != r.RoomID {
		return "", worlddomain.NotReady
	}
	if r.ExhibitID == "" {
		return c.identity.State.WorldVersion, nil
	}
	if s.world == nil {
		return "", auth.Unavailable
	}
	room, err := s.world.Geometry(r.RoomID)
	if err != nil {
		return "", auth.Unavailable
	}
	state := c.identity.State
	for _, ex := range room.Exhibits {
		if ex.ID == r.ExhibitID {
			camera := state.Position
			camera.Y += 1.65
			if math.Hypot(state.Position.X-ex.Position.X, state.Position.Z-ex.Position.Z) > 3 || !worlddomain.Visible(room, camera, ex.Position) {
				return "", auth.Code("OUT_OF_RANGE")
			}
			return state.WorldVersion, nil
		}
	}
	return "", auth.Code("NOT_FOUND")
}
