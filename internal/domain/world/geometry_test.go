package world

import (
	"math"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
)

func movementRoom() Room {
	return Room{Min: player.Vector{X: -10, Y: -1, Z: -10}, Max: player.Vector{X: 10, Y: 4, Z: 10}, Surfaces: []Surface{
		{Position: player.Vector{Y: -.1}, Size: player.Vector{X: 20, Y: .2, Z: 20}, Collidable: true},
		{Position: player.Vector{Y: 1}, Size: player.Vector{X: 2, Y: 2, Z: 2}, Collidable: true},
	}}
}

func TestSweepCylinder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to player.Vector
		want     bool
	}{
		{"crosses whole wall", player.Vector{X: -2}, player.Vector{X: 2}, false},
		{"floor contact", player.Vector{X: -3, Z: 3}, player.Vector{X: 3, Z: 3}, true},
		{"corner outside radius", player.Vector{X: 1.22, Z: 1.22}, player.Vector{X: 1.25, Z: 1.22}, true},
		{"corner inside radius", player.Vector{X: 1.2, Z: 1.2}, player.Vector{X: 1.25, Z: 1.2}, false},
		{"corner tunnelling", player.Vector{X: .8, Z: 1.5}, player.Vector{X: 1.5, Z: .8}, false},
		{"stationary safe", player.Vector{X: 2}, player.Vector{X: 2}, true},
		{"boundary", player.Vector{X: 9}, player.Vector{X: 9.71}, false},
		{"boundary tangent", player.Vector{X: 9}, player.Vector{X: 9.7}, true},
		{"nonzero y", player.Vector{X: 2}, player.Vector{X: 2, Y: .01}, false},
		{"nan", player.Vector{X: 2}, player.Vector{X: math.NaN()}, false},
		{"inf", player.Vector{X: math.Inf(-1)}, player.Vector{X: 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sweep(movementRoom(), tc.from, tc.to); got != tc.want {
				t.Fatalf("Sweep=%v want %v", got, tc.want)
			}
		})
	}
	r := movementRoom()
	r.Surfaces[1].Collidable = false
	if !Sweep(r, player.Vector{X: -2}, player.Vector{X: 2}) {
		t.Fatal("noncollidable blocks motion")
	}
	r.Surfaces[1].Collidable = true
	r.Surfaces[1].Position.Y = 3
	if !Sweep(r, player.Vector{X: -2}, player.Vector{X: 2}) {
		t.Fatal("surface above player blocks motion")
	}
}

func TestVisibleCollidableInterior(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to player.Vector
		want     bool
	}{
		{"wall", player.Vector{X: -3, Y: 1.65}, player.Vector{X: 3}, false},
		{"floor endpoint", player.Vector{X: -3, Y: 1.65, Z: 3}, player.Vector{X: 3, Z: 3}, true},
		{"tangent face", player.Vector{X: -3, Y: 1, Z: 1}, player.Vector{X: 3, Y: 1, Z: 1}, true},
		{"above wall", player.Vector{X: -3, Y: 3}, player.Vector{X: 3, Y: 3}, true},
		{"nan", player.Vector{X: math.NaN()}, player.Vector{}, false},
		{"infinite target", player.Vector{}, player.Vector{Y: math.Inf(1)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Visible(movementRoom(), tc.from, tc.to); got != tc.want {
				t.Fatalf("Visible=%v want %v", got, tc.want)
			}
		})
	}
	r := movementRoom()
	r.Surfaces[1].Collidable = false
	if !Visible(r, player.Vector{X: -3, Y: 1}, player.Vector{X: 3, Y: 1}) {
		t.Fatal("noncollidable blocks sight")
	}
}

func TestValidRotationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		rotation player.Rotation
		want     bool
	}{
		{player.Rotation{}, true}, {player.Rotation{Yaw: -math.Pi, Pitch: math.Pi / 2}, true},
		{player.Rotation{Yaw: math.Pi}, false}, {player.Rotation{Pitch: -math.Pi / 2}, true},
		{player.Rotation{Pitch: math.Pi/2 + .01}, false}, {player.Rotation{Yaw: math.NaN()}, false},
		{player.Rotation{Pitch: math.Inf(-1)}, false},
	} {
		if got := ValidRotation(tc.rotation); got != tc.want {
			t.Fatalf("rotation=%+v got %v", tc.rotation, got)
		}
	}
}
