package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// ProfileCatalog supplies validated, server-only source text.
type ProfileCatalog interface {
	Profile(string) (guide.Profile, error)
}

// ProfileStore commits profile CAS and all public room revision changes together.
// UpdateProfile shares the database scheduler lock with queued-to-running claims.
type ProfileStore interface {
	ReadProfile(context.Context, string, string) (guide.Override, bool, error)
	UpdateProfile(context.Context, int64, guide.Override, []domain.Overlay) error
}

// AdminGuide contains editable settings and prepared sources; never user conversations.
type AdminGuide struct {
	Override  guide.Override
	Materials []guide.Material
}

// ConfigureProfiles installs S02 ports before Initialize and listener startup.
func (s *Service) ConfigureProfiles(c ProfileCatalog, st ProfileStore) {
	s.profileCatalog = c
	s.profileStore = st
}

func (s *Service) seedProfiles() error {
	if s.profileCatalog == nil {
		return nil
	}
	seeds := make(map[string]guide.Override)
	for _, r := range s.catalog.Rooms() {
		for _, g := range r.Guides {
			if old, ok := seeds[g.ID]; ok {
				if old.Settings.DisplayName != g.DisplayName || old.Settings.Subtitle != g.Subtitle {
					return fmt.Errorf("ambiguous public identity for guide %q", g.ID)
				}
				continue
			}
			p, err := s.profileCatalog.Profile(g.ID)
			if err != nil {
				return err
			}
			settings, err := guide.NormalizeSettings(guide.SeedSettings(g.DisplayName, g.Subtitle, p), p)
			if err != nil {
				return fmt.Errorf("invalid guide seed %q: %w", g.ID, err)
			}
			seeds[g.ID] = guide.Override{WorldVersion: r.Version, GuideID: g.ID, Revision: 1, Settings: settings}
		}
	}
	s.seeds = seeds
	s.identities = make(map[string]guide.Settings, len(seeds))
	return nil
}

func (s *Service) readProfile(ctx context.Context, id string) (guide.Override, guide.Profile, error) {
	seed, ok := s.seeds[id]
	if !ok || s.profileStore == nil {
		return guide.Override{}, guide.Profile{}, world.NotFound
	}
	base, err := s.profileCatalog.Profile(id)
	if err != nil {
		return guide.Override{}, guide.Profile{}, err
	}
	o, exists, err := s.profileStore.ReadProfile(ctx, seed.WorldVersion, id)
	if err != nil {
		return o, base, auth.Unavailable
	}
	if !exists {
		o = seed
	}
	if o.WorldVersion != seed.WorldVersion || o.GuideID != id || o.Revision < 1 || o.Revision > world.MaxSafeInteger {
		return o, base, auth.Unavailable
	}
	o.Settings, err = guide.NormalizeSettings(o.Settings, base)
	if err != nil {
		return o, base, auth.Unavailable
	}
	return o, base, nil
}

// Guide reads durable settings independently of game ownership.
func (s *Service) Guide(ctx context.Context, id string) (AdminGuide, error) {
	release, err := s.acquire(ctx, "\x00profile/"+id)
	if err != nil {
		return AdminGuide{}, err
	}
	defer release()
	o, base, err := s.readProfile(ctx, id)
	return AdminGuide{Override: o, Materials: base.Clone().Materials}, err
}

// Guides returns each personality once, in stable guide ID order.
func (s *Service) Guides(ctx context.Context) ([]AdminGuide, error) {
	ids := make([]string, 0, len(s.seeds))
	for id := range s.seeds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]AdminGuide, 0, len(ids))
	for _, id := range ids {
		g, err := s.Guide(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *Service) refreshIdentities(ctx context.Context, r world.Room) error {
	if s.profileCatalog == nil {
		return nil
	}
	settings := make(map[string]guide.Settings, len(r.Guides))
	for _, g := range r.Guides {
		o, _, err := s.readProfile(ctx, g.ID)
		if err != nil {
			return err
		}
		settings[g.ID] = o.Settings
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range settings {
		s.identities[id] = p.Clone()
	}
	return nil
}

func (s *Service) effective(r world.Room, o domain.Overlay) domain.Room {
	result := domain.Effective(r, o)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, g := range result.Guides {
		if p, ok := s.identities[g.ID]; ok {
			result.Guides[i].DisplayName = p.DisplayName
			result.Guides[i].Subtitle = p.Subtitle
		}
	}
	return result
}

// UpdateGuide serializes all affected rooms and publishes only committed identities.
func (s *Service) UpdateGuide(ctx context.Context, id, version string, expected int64, settings guide.Settings) (AdminGuide, error) {
	if _, ok := s.seeds[id]; !ok {
		return AdminGuide{}, world.NotFound
	}
	if version != s.catalog.Manifest().Version {
		return AdminGuide{}, world.VersionMismatch
	}
	if expected < 1 || expected > world.MaxSafeInteger {
		return AdminGuide{}, auth.Validation
	}
	release, err := s.acquire(ctx, "\x00profile/"+id)
	if err != nil {
		return AdminGuide{}, err
	}
	defer release()
	old, base, err := s.readProfile(ctx, id)
	if err != nil {
		return AdminGuide{}, err
	}
	settings, err = guide.NormalizeSettings(settings, base)
	if err != nil {
		return AdminGuide{}, err
	}
	if old.Revision != expected {
		return AdminGuide{}, domain.RevisionConflict
	}
	if expected == world.MaxSafeInteger {
		return AdminGuide{}, auth.Unavailable
	}
	rooms := make([]world.Room, 0)
	for _, r := range s.catalog.Rooms() {
		for _, g := range r.Guides {
			if g.ID == id {
				rooms = append(rooms, r)
				break
			}
		}
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].ID < rooms[j].ID })
	releases := make([]func(), 0, len(rooms))
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for _, r := range rooms {
		unlock, e := s.acquire(ctx, r.ID)
		if e != nil {
			return AdminGuide{}, e
		}
		releases = append(releases, unlock)
	}
	publicChange := settings.DisplayName != old.Settings.DisplayName || settings.Subtitle != old.Settings.Subtitle
	overlays := make([]domain.Overlay, 0, len(rooms))
	results := make([]domain.Room, 0, len(rooms))
	if publicChange {
		for _, r := range rooms {
			current, e := s.read(ctx, r)
			if e != nil {
				return AdminGuide{}, e
			}
			if current.Revision == world.MaxSafeInteger {
				return AdminGuide{}, auth.Unavailable
			}
			next := current.Clone()
			next.Revision++
			result := s.effective(r, next)
			for i, g := range result.Guides {
				if g.ID == id {
					result.Guides[i].DisplayName = settings.DisplayName
					result.Guides[i].Subtitle = settings.Subtitle
				}
			}
			n, e := s.sizer.RuntimeBytes(result)
			if e != nil || n <= 0 || n > domain.MaxRuntimeBytes {
				return AdminGuide{}, auth.Validation
			}
			overlays = append(overlays, next)
			results = append(results, result)
		}
	}
	next := guide.Override{WorldVersion: version, GuideID: id, Revision: expected + 1, Settings: settings}
	if err = s.profileStore.UpdateProfile(ctx, expected, next, overlays); err != nil {
		if errors.Is(err, domain.RevisionConflict) {
			return AdminGuide{}, err
		}
		if errors.Is(err, domain.CommitUnknown) {
			s.mu.Lock()
			delete(s.identities, id)
			for _, r := range rooms {
				delete(s.cache, r.ID)
			}
			s.mu.Unlock()
			for _, r := range rooms {
				s.publisher.InvalidateRuntime(r.ID)
			}
		}
		return AdminGuide{}, auth.Unavailable
	}
	s.mu.Lock()
	s.identities[id] = settings.Clone()
	for _, o := range overlays {
		s.cache[o.RoomID] = o.Clone()
	}
	s.mu.Unlock()
	for _, r := range results {
		s.publisher.PublishRuntime(r)
	}
	return AdminGuide{Override: next, Materials: base.Clone().Materials}, nil
}
