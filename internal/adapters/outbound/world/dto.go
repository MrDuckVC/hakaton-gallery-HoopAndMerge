package world

import (
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

type vector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func (v vector) domain() player.Vector { return player.Vector{X: v.X, Y: v.Y, Z: v.Z} }

type rotation struct {
	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch"`
}
type pose struct {
	RoomID   string   `json:"room_id"`
	Position vector   `json:"position"`
	Rotation rotation `json:"rotation"`
}

func (p pose) domain(version string) player.State {
	return player.State{WorldVersion: version, RoomID: p.RoomID, Position: p.Position.domain(), Rotation: player.Rotation{Yaw: p.Rotation.Yaw, Pitch: p.Rotation.Pitch}}
}

type reference struct {
	ID   string `json:"id"`
	File string `json:"file"`
}
type index struct {
	ID       string          `json:"world_id"`
	Version  string          `json:"world_version"`
	Rooms    []reference     `json:"rooms"`
	Assets   []reference     `json:"assets"`
	Profiles []reference     `json:"profiles"`
	Start    pose            `json:"start"`
	Spawns   map[string]pose `json:"spawns"`
}
type bounds struct {
	Min vector `json:"min"`
	Max vector `json:"max"`
}
type surface struct {
	ID         string `json:"id"`
	Position   vector `json:"position"`
	Size       vector `json:"size"`
	Color      string `json:"color"`
	Collidable bool   `json:"collidable"`
}
type door struct {
	ID       string  `json:"id"`
	Target   string  `json:"target_room_id"`
	Label    string  `json:"label"`
	Position vector  `json:"position"`
	Radius   float64 `json:"interaction_radius_m"`
}
type exhibit struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	AssetID     string  `json:"image_asset_id"`
	Position    vector  `json:"position"`
	Rotation    float64 `json:"rotation_y"`
	Width       float64 `json:"width_m"`
	Height      float64 `json:"height_m"`
	Radius      float64 `json:"interaction_radius_m"`
}
type guide struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Subtitle    string  `json:"subtitle"`
	Position    vector  `json:"position"`
	Rotation    float64 `json:"rotation_y"`
	Radius      float64 `json:"interaction_radius_m"`
}
type room struct {
	ID       string    `json:"id"`
	Version  string    `json:"world_version"`
	Title    string    `json:"title"`
	Bounds   bounds    `json:"bounds"`
	Surfaces []surface `json:"surfaces"`
	Doors    []door    `json:"doors"`
	Exhibits []exhibit `json:"exhibits"`
	Guides   []guide   `json:"guides"`
}

func (r room) domain() domain.Room {
	out := domain.Room{ID: r.ID, Version: r.Version, Title: r.Title, Min: r.Bounds.Min.domain(), Max: r.Bounds.Max.domain()}
	for _, s := range r.Surfaces {
		out.Surfaces = append(out.Surfaces, domain.Surface{ID: s.ID, Position: s.Position.domain(), Size: s.Size.domain(), Color: s.Color, Collidable: s.Collidable})
	}
	for _, d := range r.Doors {
		out.Doors = append(out.Doors, domain.Door{ID: d.ID, Target: d.Target, Label: d.Label, Position: d.Position.domain(), Radius: d.Radius})
	}
	for _, e := range r.Exhibits {
		out.Exhibits = append(out.Exhibits, domain.Exhibit{ID: e.ID, Title: e.Title, Description: e.Description, AssetID: e.AssetID, Position: e.Position.domain(), Rotation: e.Rotation, Width: e.Width, Height: e.Height, Radius: e.Radius})
	}
	for _, g := range r.Guides {
		out.Guides = append(out.Guides, domain.Guide{ID: g.ID, DisplayName: g.DisplayName, Subtitle: g.Subtitle, Position: g.Position.domain(), Rotation: g.Rotation, Radius: g.Radius})
	}
	return out
}
