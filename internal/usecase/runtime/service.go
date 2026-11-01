package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Service serializes each room through a context-aware channel gate. The cache
// mutex protects memory only, and is never held across SQL or publication.
type Service struct {
	catalog   Catalog
	store     Store
	roles     RoleStore
	publisher Publisher
	sizer     Sizer
	mu        sync.RWMutex
	cache     map[string]domain.Overlay
	gates     map[string]chan struct{}
}

// New connects the runtime ports; Initialize must complete before serving.
func New(c Catalog, store Store, roles RoleStore, publisher Publisher, sizer Sizer) *Service {
	return &Service{catalog: c, store: store, roles: roles, publisher: publisher, sizer: sizer, cache: make(map[string]domain.Overlay), gates: make(map[string]chan struct{})}
}

// Initialize validates seeds and durable overlays without changing world files or schema.
func (s *Service) Initialize(ctx context.Context) error {
	for _, r := range s.catalog.Rooms() {
		if err := domain.ValidateOverlay(r, s.catalog.SpawnPositions(r.ID), domain.Seed(r)); err != nil {
			return fmt.Errorf("room %q has unsafe Sunday seed: %w", r.ID, err)
		}
		if _, err := s.Read(ctx, r.ID); err != nil {
			return fmt.Errorf("initialize runtime room %q: %w", r.ID, err)
		}
	}
	return nil
}

func (s *Service) acquire(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	gate := s.gates[id]
	if gate == nil {
		gate = make(chan struct{}, 1)
		gate <- struct{}{}
		s.gates[id] = gate
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
		if err := ctx.Err(); err != nil {
			gate <- struct{}{}
			return nil, err
		}
		return func() { gate <- struct{}{} }, nil
	}
}

// RequireAdmin checks current durable membership, independently of WS ownership.
func (s *Service) RequireAdmin(ctx context.Context, userID string) error {
	allowed, err := s.roles.IsAdmin(ctx, userID)
	if err != nil {
		return fmt.Errorf("read administrator membership: %w", auth.Unavailable)
	}
	if !allowed {
		return domain.Forbidden
	}
	return nil
}

// Rooms returns every existing room and its current durable revision.
func (s *Service) Rooms(ctx context.Context) ([]Summary, error) {
	base := s.catalog.Rooms()
	out := make([]Summary, 0, len(base))
	for _, r := range base {
		state, err := s.Read(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Summary{RoomID: r.ID, Title: r.Title, Revision: state.Revision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RoomID < out[j].RoomID })
	return out, nil
}

func (s *Service) read(ctx context.Context, r world.Room) (domain.Room, error) {
	o, exists, err := s.store.Read(ctx, r.Version, r.ID)
	if err != nil {
		return domain.Room{}, fmt.Errorf("read room runtime: %w", auth.Unavailable)
	}
	if !exists {
		o = domain.Seed(r)
	}
	if err = domain.ValidateOverlay(r, s.catalog.SpawnPositions(r.ID), o); err != nil {
		return domain.Room{}, fmt.Errorf("invalid persisted room runtime: %w", auth.Unavailable)
	}
	result := domain.Effective(r, o)
	n, err := s.sizer.RuntimeBytes(result)
	if err != nil || n <= 0 || n > domain.MaxRuntimeBytes {
		return domain.Room{}, fmt.Errorf("invalid persisted runtime size: %w", auth.Unavailable)
	}
	s.mu.Lock()
	s.cache[r.ID] = o.Clone()
	s.mu.Unlock()
	return result, nil
}

// WithRoom holds the room operation gate across a durable read and a callback.
// The callback may briefly take the game mutex to enqueue snapshot/runtime in
// order; it must never call another runtime method for this room.
func (s *Service) WithRoom(ctx context.Context, id string, fn func(domain.Room) error) error {
	r, err := s.catalog.Geometry(id)
	if err != nil {
		return err
	}
	release, err := s.acquire(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	result, err := s.read(ctx, r)
	if err != nil {
		return err
	}
	return fn(result)
}

// Read always resolves durable state, including after reconnect or lost publication.
func (s *Service) Read(ctx context.Context, id string) (domain.Room, error) {
	var result domain.Room
	err := s.WithRoom(ctx, id, func(r domain.Room) error { result = r; return nil })
	return result, err
}

// Update validates a complete config and commits its CAS before notifying sockets.
func (s *Service) Update(ctx context.Context, id, version string, expected int64, config domain.Config) (domain.Room, error) {
	r, err := s.catalog.Geometry(id)
	if err != nil {
		return domain.Room{}, err
	}
	if version != s.catalog.Manifest().Version {
		return domain.Room{}, world.VersionMismatch
	}
	if expected < 1 || expected > world.MaxSafeInteger {
		return domain.Room{}, auth.Validation
	}
	if err = domain.ValidateConfig(r, s.catalog.SpawnPositions(id), config); err != nil {
		return domain.Room{}, err
	}
	release, err := s.acquire(ctx, id)
	if err != nil {
		return domain.Room{}, err
	}
	defer release()
	current, err := s.read(ctx, r)
	if err != nil {
		return domain.Room{}, err
	}
	if current.Revision != expected {
		return domain.Room{}, domain.RevisionConflict
	}
	if current.Lamp.HolderUserID != "" {
		return domain.Room{}, domain.ObjectBusy
	}
	if expected == world.MaxSafeInteger {
		return domain.Room{}, auth.Unavailable
	}
	next := current.Clone()
	next.Revision++
	next.Config = config
	next = next.Clone()
	result := domain.Effective(r, next)
	n, err := s.sizer.RuntimeBytes(result)
	if err != nil || n <= 0 || n > domain.MaxRuntimeBytes {
		return domain.Room{}, auth.Validation
	}
	if err = s.store.CompareAndSwap(ctx, expected, next); err != nil {
		if errors.Is(err, domain.RevisionConflict) {
			return domain.Room{}, domain.RevisionConflict
		}
		if errors.Is(err, domain.ObjectBusy) {
			return domain.Room{}, domain.ObjectBusy
		}
		if errors.Is(err, domain.CommitUnknown) {
			s.mu.Lock()
			delete(s.cache, id)
			s.mu.Unlock()
			s.publisher.InvalidateRuntime(id)
		}
		return domain.Room{}, fmt.Errorf("commit room runtime: %w", auth.Unavailable)
	}
	s.mu.Lock()
	s.cache[id] = next.Clone()
	s.mu.Unlock()
	s.publisher.PublishRuntime(result)
	return result, nil
}

// Geometry returns cached effective guide placement without SQL. Invalidated
// rooms remain unavailable until a durable read resolves an uncertain commit.
func (s *Service) Geometry(id string) (world.Room, error) {
	r, err := s.catalog.Geometry(id)
	if err != nil {
		return world.Room{}, err
	}
	s.mu.RLock()
	o, ok := s.cache[id]
	if ok {
		o = o.Clone()
	}
	s.mu.RUnlock()
	if !ok {
		return world.Room{}, auth.Unavailable
	}
	r.Guides = domain.Effective(r, o).Guides
	return r, nil
}

// HasGuide recognizes a personality independently of room placement.
func (s *Service) HasGuide(id string) bool { return s.catalog.HasGuide(id) }

// Package forwards immutable package reservations without changing file budgets.
func (s *Service) Package(id string) (world.RoomPackage, error) { return s.catalog.Package(id) }

// Spawn forwards immutable door spawn geometry.
func (s *Service) Spawn(id string) (player.State, error) { return s.catalog.Spawn(id) }

// CloneRoom returns a detached state suitable for queued publication.
func CloneRoom(r domain.Room) domain.Room {
	r.Overlay = r.Clone()
	r.Guides = slices.Clone(r.Guides)
	return r
}
