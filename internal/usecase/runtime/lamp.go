package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// LampStore conditionally replaces a lamp, comparing both revision and private owner.
// Existing admin configuration writes retain their installed-only store operation.
type LampStore interface {
	CompareAndSwapLamp(context.Context, int64, domain.Holder, domain.Overlay) error
}

// Act holds the room gate through durable read, final admission and publication.
// Callbacks may only perform bounded memory operations, never acquire user gates.
func (s *Service) Act(ctx context.Context, id string, actor domain.ObjectActor, a domain.ObjectAction, guard func() error, publish func(domain.Room)) error {
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
	next, err := domain.ApplyObject(r, current.Overlay, actor, a)
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
	publish(result)
	return nil
}

func (s *Service) commitOverlay(ctx context.Context, r world.Room, current, next domain.Overlay) error {
	store, ok := s.store.(LampStore)
	if !ok {
		return auth.Unavailable
	}
	if err := store.CompareAndSwapLamp(ctx, current.Revision, current.Lamp.Owner(), next); err != nil {
		if errors.Is(err, domain.RevisionConflict) {
			return domain.RevisionConflict
		}
		if errors.Is(err, domain.CommitUnknown) {
			s.mu.Lock()
			delete(s.cache, r.ID)
			s.mu.Unlock()
			s.publisher.InvalidateRuntime(r.ID)
			readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			// Reread within the caller deadline; cancellation leaves recovery to
			// the dirty-room worker or the next startup, never extends shutdown.
			_, _ = s.read(readCtx, r)
		}
		return fmt.Errorf("commit room overlay: %w", auth.Unavailable)
	}
	s.mu.Lock()
	s.cache[r.ID] = next.Clone()
	s.mu.Unlock()
	return nil
}

// ReleaseLamp releases only the specified owner; late callbacks are harmless.
func (s *Service) ReleaseLamp(ctx context.Context, id string, owner domain.Holder) error {
	return s.cleanLamp(ctx, id, func(l domain.Lamp) bool { return l.Owner() == owner })
}

// ReconcileLamp lazily releases disconnected owners without trusting a RAM tuple.
// live must inspect the connection registry without acquiring a user gate.
func (s *Service) ReconcileLamp(ctx context.Context, id string, live func(string, string) bool) error {
	return s.cleanLamp(ctx, id, func(l domain.Lamp) bool { return !live(l.HolderUserID, l.HolderConnectionID) })
}

func (s *Service) cleanLamp(ctx context.Context, id string, stale func(domain.Lamp) bool) error {
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
	if current.Lamp.HolderUserID == "" || !stale(current.Lamp) {
		return nil
	}
	owner := current.Lamp.Owner()
	for range 3 {
		if current.Lamp.Owner() != owner {
			return nil
		}
		if current.Revision == world.MaxSafeInteger {
			return auth.Unavailable
		}
		next := current.Clone()
		next.Revision++
		next.Lamp.HolderUserID = ""
		next.Lamp.HolderConnectionID = ""
		err = s.commitOverlay(ctx, r, current.Overlay, next)
		if err == nil {
			s.publisher.PublishRuntime(s.effective(r, next))
			return nil
		}
		// Conflicts and unknown COMMIT require a fresh read, never a RAM rollback.
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		reread, readErr := s.read(readCtx, r)
		cancel()
		if readErr != nil {
			return readErr
		}
		if reread.Lamp.Owner() != owner {
			s.publisher.PublishRuntime(reread)
			return nil
		}
		current = reread
		if !errors.Is(err, domain.RevisionConflict) {
			return err
		}
	}
	return auth.Unavailable
}
