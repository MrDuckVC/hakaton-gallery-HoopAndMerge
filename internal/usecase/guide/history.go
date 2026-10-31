package guide

import (
	"context"

	auth "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
)

// History authenticates the owner independently of game readiness or location.
func (s *Service) History(ctx context.Context, token, guideID string, limit int, before string) ([]domain.Generation, string, error) {
	session, _, err := s.sessions.Authenticate(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if !s.world.HasGuide(guideID) {
		return nil, "", auth.Code("NOT_FOUND")
	}
	if limit < 1 || limit > 50 {
		return nil, "", auth.Validation
	}
	return s.store.History(ctx, session.UserID, guideID, limit, before)
}

// ByRequest recovers precisely one private question, including terminal failures.
func (s *Service) ByRequest(ctx context.Context, token, guideID, request string) (domain.Generation, error) {
	session, _, err := s.sessions.Authenticate(ctx, token)
	if err != nil {
		return domain.Generation{}, err
	}
	if !s.world.HasGuide(guideID) {
		return domain.Generation{}, auth.Code("NOT_FOUND")
	}
	g, err := s.store.ByRequest(ctx, session.UserID, request)
	if err != nil {
		return g, err
	}
	if g.GuideID != guideID {
		return domain.Generation{}, auth.Code("NOT_FOUND")
	}
	return g, nil
}
