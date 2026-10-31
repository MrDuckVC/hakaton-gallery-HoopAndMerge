// Package world defines immutable world content and its geometric invariants.
package world

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
)

const (
	PhotoReserve    int64 = 3145728
	MaxMessageBytes int64 = 262144
	MaxSafeInteger  int64 = 9007199254740991
)

// Code is a world application error mapped at the transport boundary.
type Code string

func (c Code) Error() string { return string(c) }

const (
	VersionMismatch   Code = "WORLD_VERSION_MISMATCH"
	RoomNotAccessible Code = "ROOM_NOT_ACCESSIBLE"
	NotReady          Code = "GAME_NOT_READY"
	NotFound          Code = "NOT_FOUND"
)

// Surface is an axis-aligned block; Position is its center.
type Surface struct {
	ID             string
	Position, Size player.Vector
	Color          string
	Collidable     bool
}

// Door identifies a directed interaction into another room.
type Door struct {
	ID, Target, Label string
	Position          player.Vector
	Radius            float64
}

// Exhibit references an immutable image.
type Exhibit struct {
	ID, Title, Description, AssetID string
	Position                        player.Vector
	Rotation, Width, Height, Radius float64
}

// Guide references a server-side personality profile.
type Guide struct {
	ID, DisplayName, Subtitle string
	Position                  player.Vector
	Rotation, Radius          float64
}

// Room contains geometry, interactions and public metadata.
type Room struct {
	ID, Version, Title string
	Min, Max           player.Vector
	Surfaces           []Surface
	Doors              []Door
	Exhibits           []Exhibit
	Guides             []Guide
}

// AssetInfo advertises one dependency of a room.
type AssetInfo struct {
	ID          string
	Bytes       int64
	ContentType string
}

// RoomPackage contains exact byte reservations, with unique assets.
type RoomPackage struct {
	RoomID          string
	DescriptorBytes int64
	Assets          []AssetInfo
}

// Manifest contains world-wide immutable accounting.
type Manifest struct {
	ID, Version            string
	TotalBytes, CacheLimit int64
}

// Input is the semantic content, independent of file layout and JSON.
type Input struct {
	ID, Version string
	Rooms       []Room
	Assets      []string
	Profiles    []string
	Start       player.State
	Spawns      map[string]player.State
}

// ValidID checks the contract's printable ASCII opaque identifier.
func ValidID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c < '!' || c > '~' {
			return false
		}
	}
	return true
}

// ValidResourceID rejects dot segments that HTTP cannot safely address as resources.
func ValidResourceID(id string) bool {
	if !ValidID(id) {
		return false
	}
	for _, segment := range strings.Split(strings.ReplaceAll(id, "\\", "/"), "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func unique(ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !ValidID(id) || out[id] {
			return nil, fmt.Errorf("invalid or duplicate ID %q", id)
		}
		out[id] = true
	}
	return out, nil
}

// Validate checks references, safe directed spawns and a reversible connected room graph.
func Validate(in Input) error {
	if !ValidID(in.ID) || !ValidID(in.Version) || len(in.Rooms) == 0 {
		return errors.New("invalid world identity or empty rooms")
	}
	assets, err := unique(in.Assets)
	if err != nil {
		return err
	}
	for _, id := range in.Assets {
		if !ValidResourceID(id) {
			return fmt.Errorf("invalid resource ID %q", id)
		}
	}
	profiles, err := unique(in.Profiles)
	if err != nil {
		return err
	}
	rooms := make(map[string]Room, len(in.Rooms))
	doors := make(map[string]bool)
	exhibits := make(map[string]bool)
	for _, r := range in.Rooms {
		if !ValidResourceID(r.ID) || rooms[r.ID].ID != "" || r.Version != in.Version || r.Title == "" {
			return fmt.Errorf("invalid room %q", r.ID)
		}
		if err := validateRoom(r, assets, profiles, doors, exhibits); err != nil {
			return fmt.Errorf("room %q: %w", r.ID, err)
		}
		rooms[r.ID] = r
	}
	if in.Start.WorldVersion != in.Version || rooms[in.Start.RoomID].ID == "" || !ValidPose(rooms[in.Start.RoomID], in.Start) {
		return errors.New("unsafe start")
	}
	if len(in.Spawns) != len(doors) {
		return errors.New("missing or extra directed spawns")
	}
	for _, r := range in.Rooms {
		for _, d := range r.Doors {
			target, ok := rooms[d.Target]
			spawn, exists := in.Spawns[d.ID]
			if !ok || d.Target == r.ID || !exists || spawn.WorldVersion != in.Version || spawn.RoomID != d.Target || !ValidPose(target, spawn) {
				return fmt.Errorf("door %q: invalid target or spawn", d.ID)
			}
		}
	}
	// Forward and reverse reachability from start prove strong connectivity.
	for _, reverse := range []bool{false, true} {
		seen := map[string]bool{in.Start.RoomID: true}
		queue := []string{in.Start.RoomID}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			for _, r := range in.Rooms {
				for _, d := range r.Doors {
					from, to := r.ID, d.Target
					if reverse {
						from, to = to, from
					}
					if from == id && !seen[to] {
						seen[to] = true
						queue = append(queue, to)
					}
				}
			}
		}
		if len(seen) != len(rooms) {
			return errors.New("room graph is not reversible and connected")
		}
	}
	return nil
}

func validateRoom(r Room, assets, profiles, doors, exhibits map[string]bool) error {
	if !vectorFinite(r.Min) || !vectorFinite(r.Max) || r.Min.Y != 0 || r.Max.X-r.Min.X < 0.6 || r.Max.Z-r.Min.Z < 0.6 || r.Max.Y < 1.8 {
		return errors.New("invalid bounds")
	}
	surfaces := make([]string, 0, len(r.Surfaces))
	color := regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	for _, s := range r.Surfaces {
		surfaces = append(surfaces, s.ID)
		if !vectorFinite(s.Position) || !vectorFinite(s.Size) || s.Size.X <= 0 || s.Size.Y <= 0 || s.Size.Z <= 0 || !color.MatchString(s.Color) {
			return errors.New("invalid surface")
		}
	}
	if _, err := unique(surfaces); err != nil {
		return err
	}
	if len(r.Doors) == 0 {
		return errors.New("room has no exit")
	}
	for _, d := range r.Doors {
		if !ValidID(d.ID) || doors[d.ID] || d.Radius != 1.5 || !ValidPose(r, player.State{Position: d.Position}) {
			return errors.New("invalid door")
		}
		doors[d.ID] = true
	}
	for _, e := range r.Exhibits {
		if !ValidID(e.ID) || exhibits[e.ID] || !assets[e.AssetID] || !pointInside(r, e.Position) || !finite(e.Rotation) || !finite(e.Width) || !finite(e.Height) || e.Width <= 0 || e.Height <= 0 || e.Radius != 3 {
			return errors.New("invalid exhibit")
		}
		exhibits[e.ID] = true
	}
	guides := make([]string, 0, len(r.Guides))
	for _, g := range r.Guides {
		guides = append(guides, g.ID)
		if !profiles[g.ID] || !pointInside(r, g.Position) || !finite(g.Rotation) || g.Radius != 3 {
			return errors.New("invalid guide")
		}
	}
	_, err := unique(guides)
	return err
}
