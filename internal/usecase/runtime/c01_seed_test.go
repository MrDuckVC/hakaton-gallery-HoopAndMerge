package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/runtime"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestC01SeedsFitEveryRoomWithoutChangingContent(t *testing.T) {
	root := "../../../content/world"
	data, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Rooms []struct{ ID, File string }
		Start struct {
			RoomID   string `json:"room_id"`
			Position player.Vector
		}
		Spawns map[string]struct {
			RoomID   string `json:"room_id"`
			Position player.Vector
		}
	}
	if err = json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Rooms) != 24 {
		t.Fatalf("rooms %d", len(catalog.Rooms))
	}
	for _, ref := range catalog.Rooms {
		data, err = os.ReadFile(filepath.Join(root, ref.File))
		if err != nil {
			t.Fatal(err)
		}
		var dto struct {
			ID       string
			Version  string `json:"world_version"`
			Bounds   struct{ Min, Max player.Vector }
			Surfaces []struct {
				ID, Color      string
				Position, Size player.Vector
				Collidable     bool
			}
			Guides []struct {
				ID       string
				Position player.Vector
				Rotation float64 `json:"rotation_y"`
			}
			Doors []struct {
				ID       string
				Position player.Vector
			}
		}
		if err = json.Unmarshal(data, &dto); err != nil {
			t.Fatal(err)
		}
		r := world.Room{ID: dto.ID, Version: dto.Version, Min: dto.Bounds.Min, Max: dto.Bounds.Max}
		for _, s := range dto.Surfaces {
			r.Surfaces = append(r.Surfaces, world.Surface{ID: s.ID, Color: s.Color, Position: s.Position, Size: s.Size, Collidable: s.Collidable})
		}
		for _, g := range dto.Guides {
			r.Guides = append(r.Guides, world.Guide{ID: g.ID, Position: g.Position, Rotation: g.Rotation})
		}
		for _, d := range dto.Doors {
			r.Doors = append(r.Doors, world.Door{ID: d.ID, Position: d.Position})
		}
		var spawns []player.Vector
		if catalog.Start.RoomID == r.ID {
			spawns = append(spawns, catalog.Start.Position)
		}
		for _, s := range catalog.Spawns {
			if s.RoomID == r.ID {
				spawns = append(spawns, s.Position)
			}
		}
		if got := domain.Seed(r).Config.NPCCount; got != 5 {
			t.Errorf("room %s: NPC count=%d, want 5", r.ID, got)
		}
		if err := domain.ValidateOverlay(r, spawns, domain.Seed(r)); err != nil {
			t.Errorf("room %s: %v", r.ID, err)
		}
	}
}
