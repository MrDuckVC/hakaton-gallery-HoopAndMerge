package world

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	guide "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/guide"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Catalog owns immutable content. No mutable reference escapes its public methods.
type Catalog struct {
	profiles            map[string]guide.Profile
	manifest            domain.Manifest
	start               player.State
	rooms               map[string]domain.Room
	descriptors, assets map[string]File
	packages            map[string]domain.RoomPackage
	spawns              map[string]player.State
}

// Load validates a complete catalog before the server accepts connections.
func Load(ctx context.Context, source Source, sizer SnapshotSizer, version string) (*Catalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loaded, err := source.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	in := loaded.Input
	if in.Version != version {
		return nil, domain.VersionMismatch
	}
	if err := domain.Validate(in); err != nil {
		return nil, fmt.Errorf("validate world: %w", err)
	}
	c := &Catalog{spawns: maps.Clone(in.Spawns), manifest: domain.Manifest{ID: in.ID, Version: in.Version}, start: in.Start, rooms: make(map[string]domain.Room), descriptors: make(map[string]File), assets: make(map[string]File), packages: make(map[string]domain.RoomPackage)}
	c.profiles = make(map[string]guide.Profile, len(in.Profiles))
	exhibits := make(map[string]bool)
	for _, r := range in.Rooms {
		for _, e := range r.Exhibits {
			exhibits[e.ID] = true
		}
	}
	for _, id := range in.Profiles {
		p, ok := loaded.Profiles[id]
		if !ok {
			return nil, fmt.Errorf("missing profile %q", id)
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("profile %q: %w", id, err)
		}
		for _, m := range p.Materials {
			for _, ex := range m.ExhibitIDs {
				if !exhibits[ex] {
					return nil, fmt.Errorf("profile %q references unknown exhibit", id)
				}
			}
		}
		c.profiles[id] = p.Clone()
	}
	identities := make(map[string]bool)
	add := func(f File) error {
		if f.Identity == "" || len(f.Bytes) == 0 {
			return errors.New("missing file bytes or identity")
		}
		// A file has exactly one public identity; aliases cannot inflate the denominator
		// or make multiple advertised IDs describe a single cache reservation.
		if identities[f.Identity] {
			return errors.New("file registered with multiple IDs")
		}
		identities[f.Identity] = true
		size := int64(len(f.Bytes))
		if size > domain.MaxSafeInteger-c.manifest.TotalBytes {
			return errors.New("world byte count overflow")
		}
		c.manifest.TotalBytes += size
		return nil
	}
	for _, r := range in.Rooms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file := loaded.Rooms[r.ID]
		if file.ContentType != "application/json" {
			return nil, errors.New("invalid descriptor type")
		}
		if err := add(file); err != nil {
			return nil, err
		}
		c.descriptors[r.ID] = cloneFile(file)
		// Deep-copy geometry so the source cannot mutate future pose checks.
		r.Surfaces = slices.Clone(r.Surfaces)
		r.Doors = slices.Clone(r.Doors)
		r.Exhibits = slices.Clone(r.Exhibits)
		r.Guides = slices.Clone(r.Guides)
		c.rooms[r.ID] = r
		pkg := domain.RoomPackage{RoomID: r.ID, DescriptorBytes: int64(len(file.Bytes)), Assets: []domain.AssetInfo{}}
		seen := make(map[string]bool)
		for _, e := range r.Exhibits {
			if seen[e.AssetID] {
				continue
			}
			seen[e.AssetID] = true
			a, exists := c.assets[e.AssetID]
			if !exists {
				a = loaded.Assets[e.AssetID]
				if a.ContentType != "image/jpeg" && a.ContentType != "image/webp" {
					return nil, errors.New("invalid image type")
				}
				if err := add(a); err != nil {
					return nil, err
				}
				a = cloneFile(a)
				c.assets[e.AssetID] = a
			}
			pkg.Assets = append(pkg.Assets, domain.AssetInfo{ID: e.AssetID, Bytes: int64(len(a.Bytes)), ContentType: a.ContentType})
		}
		slices.SortFunc(pkg.Assets, func(a, b domain.AssetInfo) int {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
		c.packages[r.ID] = pkg
	}
	c.manifest.CacheLimit = c.manifest.TotalBytes / 10
	for _, pkg := range c.packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := domain.CheckBudget(c.manifest.TotalBytes, pkg); err != nil {
			return nil, fmt.Errorf("room %q: %w", pkg.RoomID, err)
		}
		size, err := sizer.MaxSnapshotBytes(clonePackage(pkg))
		if err != nil {
			return nil, fmt.Errorf("measure snapshot: %w", err)
		}
		if size <= 0 || size > domain.MaxMessageBytes {
			return nil, fmt.Errorf("room %q exceeds snapshot limit", pkg.RoomID)
		}
	}
	return c, nil
}
func cloneFile(f File) File { f.Bytes = bytes.Clone(f.Bytes); return f }
func clonePackage(p domain.RoomPackage) domain.RoomPackage {
	p.Assets = slices.Clone(p.Assets)
	return p
}

// Manifest returns immutable accounting values.
func (c *Catalog) Manifest() domain.Manifest { return c.manifest }

// Package returns an independent reservation list suitable for a game snapshot.
func (c *Catalog) Package(id string) (domain.RoomPackage, error) {
	p, ok := c.packages[id]
	if !ok {
		return domain.RoomPackage{}, domain.NotFound
	}
	return clonePackage(p), nil
}

// Descriptor returns the original JSON bytes, without reformatting.
func (c *Catalog) Descriptor(id string) (File, error) {
	f, ok := c.descriptors[id]
	if !ok {
		return File{}, domain.NotFound
	}
	return cloneFile(f), nil
}

// Asset returns a copy of the exact validated image bytes.
func (c *Catalog) Asset(id string) (File, error) {
	f, ok := c.assets[id]
	if !ok {
		return File{}, domain.NotFound
	}
	return cloneFile(f), nil
}

// Start supplies auth with a verified initial pose.
func (c *Catalog) Start(ctx context.Context) (player.State, error) { return c.start, ctx.Err() }

// Valid checks stored state against the version and stationary collision geometry.
func (c *Catalog) Valid(ctx context.Context, s player.State) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r, ok := c.rooms[s.RoomID]
	return ok && s.WorldVersion == c.manifest.Version && domain.ValidPose(r, s), nil
}

// Spawn returns the validated target pose for a directed door, for the game transition usecase.
func (c *Catalog) Spawn(doorID string) (player.State, error) {
	s, ok := c.spawns[doorID]
	if !ok {
		return player.State{}, domain.NotFound
	}
	return s, nil
}

// Geometry returns an independent copy of the validated room geometry.
func (c *Catalog) Geometry(id string) (domain.Room, error) {
	r, ok := c.rooms[id]
	if !ok {
		return domain.Room{}, domain.NotFound
	}
	r.Surfaces = slices.Clone(r.Surfaces)
	r.Doors = slices.Clone(r.Doors)
	r.Exhibits = slices.Clone(r.Exhibits)
	r.Guides = slices.Clone(r.Guides)
	return r, nil
}

// HasGuide recognizes a personality independently of the current room.
func (c *Catalog) HasGuide(id string) bool {
	for _, r := range c.rooms {
		for _, g := range r.Guides {
			if g.ID == id {
				return true
			}
		}
	}
	return false
}

// Profile returns server-only personality data, independent of room occupancy.
func (c *Catalog) Profile(id string) (guide.Profile, error) {
	p, ok := c.profiles[id]
	if !ok {
		return guide.Profile{}, domain.NotFound
	}
	return p.Clone(), nil
}

// Exhibit resolves saved action IDs against the current immutable content version.
func (c *Catalog) Exhibit(id string) (domain.Exhibit, error) {
	for _, r := range c.rooms {
		for _, e := range r.Exhibits {
			if e.ID == id {
				return e, nil
			}
		}
	}
	return domain.Exhibit{}, domain.NotFound
}

// Rooms returns independent room geometry in deterministic ID order.
func (c *Catalog) Rooms() []domain.Room {
	ids := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	rooms := make([]domain.Room, 0, len(ids))
	for _, id := range ids {
		r, _ := c.Geometry(id)
		rooms = append(rooms, r)
	}
	return rooms
}

// SpawnPositions includes the start and every incoming directed-door spawn.
func (c *Catalog) SpawnPositions(roomID string) []player.Vector {
	positions := make([]player.Vector, 0, len(c.spawns)+1)
	if c.start.RoomID == roomID {
		positions = append(positions, c.start.Position)
	}
	for _, spawn := range c.spawns {
		if spawn.RoomID == roomID {
			positions = append(positions, spawn.Position)
		}
	}
	return positions
}
