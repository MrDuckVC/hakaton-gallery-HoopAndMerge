// Package runtime defines durable room overlays independently of storage and transport.
package runtime

import (
	"math"
	"slices"
	"strings"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// Code is a safe runtime application error.
type Code string

func (c Code) Error() string { return string(c) }

const (
	RevisionConflict Code = "REVISION_CONFLICT"
	ObjectBusy       Code = "OBJECT_BUSY"
	CommitUnknown    Code = "COMMIT_UNKNOWN"
	Forbidden        Code = "FORBIDDEN"
	MaxRuntimeBytes       = 32768
)

// GuidePlacement replaces one existing guide's immutable placement.
type GuidePlacement struct {
	GuideID   string
	Position  player.Vector
	RotationY float64
}

// LampSlot is one of the two legal positions of the room lamp.
type LampSlot struct {
	SlotID   string
	Position player.Vector
}

// Config is the complete editable room configuration.
type Config struct {
	AccentColor, Lighting string
	GuidePlacements       []GuidePlacement
	LampSlots             []LampSlot
	NPCCount              int
	InstallationVisible   bool
}

// Lamp contains persistent public lamp state and private connection ownership.
type Lamp struct {
	ObjectID, SlotID, HolderUserID, HolderConnectionID string
	Yaw, HeadPitch                                     float64
	On                                                 bool
}

// Overlay is the CAS-protected durable portion of room state.
type Overlay struct {
	WorldVersion, RoomID string
	Revision             int64
	Config               Config
	Lamp                 Lamp
}

// Room includes the complete public effective guide metadata.
type Room struct {
	Overlay
	Guides []world.Guide
}

// Seed constructs the specified Sunday defaults without modifying base content.
func Seed(r world.Room) Overlay {
	placements := make([]GuidePlacement, len(r.Guides))
	for i, g := range r.Guides {
		placements[i] = GuidePlacement{GuideID: g.ID, Position: g.Position, RotationY: g.Rotation}
	}
	return Overlay{WorldVersion: r.Version, RoomID: r.ID, Revision: 1, Config: Config{AccentColor: "#D9D2C3", Lighting: "neutral", GuidePlacements: placements, LampSlots: []LampSlot{{SlotID: "a", Position: player.Vector{X: 3, Z: 4}}, {SlotID: "b", Position: player.Vector{X: 9, Z: 4}}}, NPCCount: 40}, Lamp: Lamp{ObjectID: "lamp_01", SlotID: "a", On: true}}
}

// Clone returns mutable configuration storage belonging only to the caller.
func (o Overlay) Clone() Overlay {
	o.Config.GuidePlacements = slices.Clone(o.Config.GuidePlacements)
	o.Config.LampSlots = slices.Clone(o.Config.LampSlots)
	return o
}

// Effective combines immutable metadata with complete replacement placements.
func Effective(r world.Room, o Overlay) Room {
	guides := slices.Clone(r.Guides)
	for i := range guides {
		for _, p := range o.Config.GuidePlacements {
			if guides[i].ID == p.GuideID {
				guides[i].Position = p.Position
				guides[i].Rotation = p.RotationY
			}
		}
	}
	return Room{Overlay: o.Clone(), Guides: guides}
}

func finite(v float64) bool               { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func distance(a, b player.Vector) float64 { return math.Hypot(a.X-b.X, a.Z-b.Z) }
func insideMargin(r world.Room, p player.Vector) bool {
	if !finite(p.X) || !finite(p.Y) || !finite(p.Z) || p.Y != 0 || p.X < r.Min.X+.5 || p.X > r.Max.X-.5 || p.Z < r.Min.Z+.5 || p.Z > r.Max.Z-.5 {
		return false
	}
	return true
}

func safePoint(r world.Room, p player.Vector) bool {
	if !insideMargin(r, p) {
		return false
	}
	for _, s := range r.Surfaces {
		if !s.Collidable || s.Position.Y+s.Size.Y/2 <= 0 || s.Position.Y-s.Size.Y/2 >= 1.8 {
			continue
		}
		dx := math.Max(math.Abs(p.X-s.Position.X)-s.Size.X/2, 0)
		dz := math.Max(math.Abs(p.Z-s.Position.Z)-s.Size.Z/2, 0)
		if dx*dx+dz*dz < .5*.5 {
			return false
		}
	}
	return true
}

// safeInstallation checks the declared 1x1.5x0.5m rectangular stand.
func safeInstallation(r world.Room, p player.Vector) bool {
	if !insideMargin(r, p) {
		return false
	}
	for _, s := range r.Surfaces {
		if !s.Collidable || s.Position.Y+s.Size.Y/2 <= 0 || s.Position.Y-s.Size.Y/2 >= 1.5 {
			continue
		}
		if math.Abs(p.X-s.Position.X) < .5+s.Size.X/2 && math.Abs(p.Z-s.Position.Z) < .25+s.Size.Z/2 {
			return false
		}
	}
	return true
}

// ValidateConfig checks full replacement structure and current immutable geometry.
func ValidateConfig(r world.Room, spawns []player.Vector, c Config) error {
	if len(c.AccentColor) != 7 || c.AccentColor[0] != '#' || c.NPCCount < 0 || c.NPCCount > 40 {
		return auth.Validation
	}
	for _, b := range c.AccentColor[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", b) {
			return auth.Validation
		}
	}
	if c.Lighting != "neutral" && c.Lighting != "warm" && c.Lighting != "cool" {
		return auth.Validation
	}
	if len(c.GuidePlacements) != len(r.Guides) || len(c.LampSlots) != 2 {
		return auth.Validation
	}
	seen := make(map[string]bool, len(c.GuidePlacements))
	for _, p := range c.GuidePlacements {
		known := false
		for _, g := range r.Guides {
			if g.ID == p.GuideID {
				known = true
				break
			}
		}
		if !known || seen[p.GuideID] || !safePoint(r, p.Position) || !finite(p.RotationY) || p.RotationY < -math.Pi || p.RotationY >= math.Pi {
			return auth.Validation
		}
		for _, door := range r.Doors {
			if distance(p.Position, door.Position) < 1 {
				return auth.Validation
			}
		}
		for _, spawn := range spawns {
			if distance(p.Position, spawn) < 1 {
				return auth.Validation
			}
		}
		seen[p.GuideID] = true
	}
	objects := []player.Vector{{X: 6, Z: 3}}
	slots := map[string]bool{}
	for _, slot := range c.LampSlots {
		if (slot.SlotID != "a" && slot.SlotID != "b") || slots[slot.SlotID] {
			return auth.Validation
		}
		slots[slot.SlotID] = true
		objects = append(objects, slot.Position)
	}
	for i, p := range objects {
		if (i == 0 && !safeInstallation(r, p)) || (i > 0 && !safePoint(r, p)) {
			return auth.Validation
		}
		for _, d := range r.Doors {
			if distance(p, d.Position) < 1 {
				return auth.Validation
			}
		}
		for _, spawn := range spawns {
			if distance(p, spawn) < 1 {
				return auth.Validation
			}
		}
		for _, g := range c.GuidePlacements {
			if distance(p, g.Position) < 1 {
				return auth.Validation
			}
		}
		for _, other := range objects[:i] {
			if distance(p, other) < 1 {
				return auth.Validation
			}
		}
	}
	return nil
}

// ValidateOverlay prevents corrupt or foreign durable state from becoming effective.
func ValidateOverlay(r world.Room, spawns []player.Vector, o Overlay) error {
	if o.RoomID != r.ID || o.WorldVersion != r.Version || o.Revision < 1 || o.Revision > world.MaxSafeInteger {
		return auth.Validation
	}
	if err := ValidateConfig(r, spawns, o.Config); err != nil {
		return err
	}
	l := o.Lamp
	if l.ObjectID != "lamp_01" || (l.SlotID != "a" && l.SlotID != "b") || !finite(l.Yaw) || l.Yaw < -math.Pi || l.Yaw >= math.Pi || !finite(l.HeadPitch) || math.Abs(l.HeadPitch) > math.Pi/3 || (l.HolderUserID == "") != (l.HolderConnectionID == "") {
		return auth.Validation
	}
	return nil
}
