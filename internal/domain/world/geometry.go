package world

import (
	"math"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
)

func finite(n float64) bool             { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func vectorFinite(v player.Vector) bool { return finite(v.X) && finite(v.Y) && finite(v.Z) }
func pointInside(r Room, p player.Vector) bool {
	return vectorFinite(p) && p.X >= r.Min.X && p.X <= r.Max.X && p.Y >= r.Min.Y && p.Y <= r.Max.Y && p.Z >= r.Min.Z && p.Z <= r.Max.Z
}

// ValidPose tests a stationary player cylinder; tangent floor contact is permitted.
func ValidPose(r Room, s player.State) bool {
	p := s.Position
	rot := s.Rotation
	if !vectorFinite(p) || p.Y != 0 || !finite(rot.Yaw) || !finite(rot.Pitch) || rot.Yaw < -math.Pi || rot.Yaw >= math.Pi || rot.Pitch < -math.Pi/2 || rot.Pitch > math.Pi/2 {
		return false
	}
	if p.X-0.3 < r.Min.X || p.X+0.3 > r.Max.X || p.Z-0.3 < r.Min.Z || p.Z+0.3 > r.Max.Z || r.Min.Y > 0 || r.Max.Y < 1.8 {
		return false
	}
	for _, b := range r.Surfaces {
		if !b.Collidable || b.Position.Y+b.Size.Y/2 <= 0 || b.Position.Y-b.Size.Y/2 >= 1.8 {
			continue
		}
		dx := math.Max(math.Abs(p.X-b.Position.X)-b.Size.X/2, 0)
		dz := math.Max(math.Abs(p.Z-b.Position.Z)-b.Size.Z/2, 0)
		if dx*dx+dz*dz < 0.3*0.3 {
			return false
		}
	}
	return true
}
