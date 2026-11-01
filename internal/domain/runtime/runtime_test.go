package runtime

import (
	"errors"
	"math"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func fixtureRoom() world.Room {
	return world.Room{ID: "r", Version: "v", Min: player.Vector{}, Max: player.Vector{X: 12, Y: 4, Z: 12}, Guides: []world.Guide{{ID: "g", Position: player.Vector{X: 6, Z: 7.5}, Radius: 3}}, Doors: []world.Door{{ID: "d", Position: player.Vector{X: 1, Z: 6}}}}
}
func TestConfigRejectsUnsafeAndIncompletePlacement(t *testing.T) {
	room := fixtureRoom()
	cases := map[string]func(*Config){
		"guide covers door":          func(c *Config) { c.GuidePlacements[0].Position = room.Doors[0].Position },
		"guide covers spawn":         func(c *Config) { c.GuidePlacements[0].Position = player.Vector{X: 2, Z: 2} },
		"missing guide":              func(c *Config) { c.GuidePlacements = nil },
		"unknown guide":              func(c *Config) { c.GuidePlacements[0].GuideID = "other" },
		"duplicate slots":            func(c *Config) { c.LampSlots[1].SlotID = "a" },
		"guide covers slot":          func(c *Config) { c.GuidePlacements[0].Position = c.LampSlots[0].Position },
		"slot covers installation":   func(c *Config) { c.LampSlots[0].Position = player.Vector{X: 6, Z: 3} },
		"slot covers spawn":          func(c *Config) { c.LampSlots[0].Position = player.Vector{X: 2, Z: 2} },
		"outside margin":             func(c *Config) { c.GuidePlacements[0].Position.X = .49 },
		"above floor":                func(c *Config) { c.GuidePlacements[0].Position.Y = .1 },
		"nan":                        func(c *Config) { c.LampSlots[0].Position.X = math.NaN() },
		"rotation":                   func(c *Config) { c.GuidePlacements[0].RotationY = math.Inf(1) },
		"rotation at upper bound":    func(c *Config) { c.GuidePlacements[0].RotationY = math.Pi },
		"rotation below lower bound": func(c *Config) { c.GuidePlacements[0].RotationY = -math.Pi - .001 },
		"npc count":                  func(c *Config) { c.NPCCount = 41 },
		"color":                      func(c *Config) { c.AccentColor = "#GG1122" },
		"lighting":                   func(c *Config) { c.Lighting = "red" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			c := Seed(room).Config
			edit(&c)
			if err := ValidateConfig(room, []player.Vector{{X: 2, Z: 2}}, c); !errors.Is(err, auth.Validation) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if err := ValidateConfig(room, []player.Vector{{X: 2, Z: 2}}, Seed(room).Config); err != nil {
		t.Fatal(err)
	}
}
func TestConfigRejectsCollidableSurfaces(t *testing.T) {
	room := fixtureRoom()
	room.Surfaces = []world.Surface{{Collidable: true, Position: player.Vector{X: 3, Y: 1, Z: 4}, Size: player.Vector{X: .5, Y: 2, Z: .5}}}
	if err := ValidateConfig(room, nil, Seed(room).Config); !errors.Is(err, auth.Validation) {
		t.Fatalf("got %v", err)
	}
}

func TestInstallationUsesRectangularFootprint(t *testing.T) {
	r := fixtureRoom()
	// The corner (6.49,3.24) is inside the stand but outside a radius-.5 circle.
	r.Surfaces = []world.Surface{{Collidable: true, Position: player.Vector{X: 6.49, Y: .75, Z: 3.24}, Size: player.Vector{X: .01, Y: 1.5, Z: .01}}}
	if err := ValidateConfig(r, nil, Seed(r).Config); !errors.Is(err, auth.Validation) {
		t.Fatal("installation corner overlap accepted")
	}
	// A block .4m from its centre in Z is outside the 0.5m-deep stand.
	r.Surfaces[0].Position = player.Vector{X: 6, Y: .75, Z: 3.4}
	if err := ValidateConfig(r, nil, Seed(r).Config); err != nil {
		t.Fatalf("non-overlapping stand rejected: %v", err)
	}
}

func TestGuideYawLowerBoundAccepted(t *testing.T) {
	r := fixtureRoom()
	c := Seed(r).Config
	c.GuidePlacements[0].RotationY = -math.Pi
	if err := ValidateConfig(r, nil, c); err != nil {
		t.Fatalf("lower yaw bound rejected: %v", err)
	}
}
