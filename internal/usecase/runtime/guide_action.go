package runtime

import (
	"context"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

// Action commits a locally classified exhibition change under the shared room gate.
// Callbacks only inspect connection state or enqueue bounded notifications.
func (s *Service) Action(ctx context.Context, id string, position player.Vector, a domain.GuideAction, guard func() error, publish func(domain.Room, domain.GuideEffect)) error {
	r, err := s.catalog.Geometry(id)
	if err != nil {
		return err
	}
	unlock, err := s.acquire(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := s.read(ctx, r)
	if err != nil {
		return err
	}
	if err = guard(); err != nil {
		return err
	}
	next, effect, err := domain.ApplyGuideAction(r, current.Overlay, position, a)
	if err != nil {
		return err
	}
	result := s.effective(r, next)
	n, err := s.sizer.RuntimeBytes(result)
	if err != nil || n <= 0 || n > domain.MaxRuntimeBytes {
		return auth.Validation
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = guard(); err != nil {
		return err
	}
	if err = s.commitOverlay(ctx, r, current.Overlay, next); err != nil {
		return err
	}
	publish(result, effect)
	return nil
}
