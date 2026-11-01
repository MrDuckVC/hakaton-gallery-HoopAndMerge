// Package runtimewire maps room runtime to the public Sunday wire format.
package runtimewire

import (
	"encoding/json"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
)

// Vector is a floor position in the immutable world's coordinate system.
type Vector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// GuidePlacement is an editable placement of an existing guide.
type GuidePlacement struct {
	GuideID   string  `json:"guide_id"`
	Position  Vector  `json:"position"`
	RotationY float64 `json:"rotation_y"`
}

// LampSlot is one of two editable lamp locations.
type LampSlot struct {
	SlotID   string `json:"slot_id"`
	Position Vector `json:"position"`
}

// Config is the complete editable room configuration.
type Config struct {
	AccentColor         string           `json:"accent_color"`
	Lighting            string           `json:"lighting"`
	GuidePlacements     []GuidePlacement `json:"guide_placements"`
	LampSlots           []LampSlot       `json:"lamp_slots"`
	NPCCount            int              `json:"npc_count"`
	InstallationVisible bool             `json:"installation_visible"`
}

// Guide contains only public identity and effective placement.
type Guide struct {
	GuideID     string  `json:"guide_id"`
	DisplayName string  `json:"display_name"`
	Subtitle    string  `json:"subtitle"`
	Position    Vector  `json:"position"`
	RotationY   float64 `json:"rotation_y"`
}

// Lamp omits the private connection ID and represents an unheld object with null.
type Lamp struct {
	ObjectID     string  `json:"object_id"`
	SlotID       string  `json:"slot_id"`
	HolderUserID *string `json:"holder_user_id"`
	Yaw          float64 `json:"yaw"`
	HeadPitch    float64 `json:"head_pitch"`
	On           bool    `json:"on"`
}

// Room is the full public runtime payload, never a patch.
type Room struct {
	WorldVersion string  `json:"world_version"`
	RoomID       string  `json:"room_id"`
	Revision     int64   `json:"revision"`
	Config       Config  `json:"config"`
	Guides       []Guide `json:"guides"`
	Lamp         Lamp    `json:"lamp"`
}

func vector(v player.Vector) Vector { return Vector{X: v.X, Y: v.Y, Z: v.Z} }

// Public maps domain state without leaking persistence-only fields.
func Public(r domain.Room) Room {
	config := Config{AccentColor: r.Config.AccentColor, Lighting: r.Config.Lighting, GuidePlacements: make([]GuidePlacement, len(r.Config.GuidePlacements)), LampSlots: make([]LampSlot, len(r.Config.LampSlots)), NPCCount: r.Config.NPCCount, InstallationVisible: r.Config.InstallationVisible}
	for i, g := range r.Config.GuidePlacements {
		config.GuidePlacements[i] = GuidePlacement{GuideID: g.GuideID, Position: vector(g.Position), RotationY: g.RotationY}
	}
	for i, slot := range r.Config.LampSlots {
		config.LampSlots[i] = LampSlot{SlotID: slot.SlotID, Position: vector(slot.Position)}
	}
	guides := make([]Guide, len(r.Guides))
	for i, g := range r.Guides {
		guides[i] = Guide{GuideID: g.ID, DisplayName: g.DisplayName, Subtitle: g.Subtitle, Position: vector(g.Position), RotationY: g.Rotation}
	}
	lamp := Lamp{ObjectID: r.Lamp.ObjectID, SlotID: r.Lamp.SlotID, Yaw: r.Lamp.Yaw, HeadPitch: r.Lamp.HeadPitch, On: r.Lamp.On}
	if r.Lamp.HolderUserID != "" {
		holder := r.Lamp.HolderUserID
		lamp.HolderUserID = &holder
	}
	return Room{WorldVersion: r.WorldVersion, RoomID: r.RoomID, Revision: r.Revision, Config: config, Guides: guides, Lamp: lamp}
}

// Sizer measures the exact public JSON bytes before an overlay can be committed.
type Sizer struct{}

// RuntimeBytes includes the complete payload and excludes the WS envelope.
func (Sizer) RuntimeBytes(r domain.Room) (int, error) {
	body, err := json.Marshal(Public(r))
	return len(body), err
}
