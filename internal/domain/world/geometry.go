package world

import (
	"math"
	"slices"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
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
	if !vectorFinite(p) || p.Y != 0 || !ValidRotation(rot) {
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

// ValidRotation checks the protocol's finite view angles in radians.
func ValidRotation(r player.Rotation) bool {
	return finite(r.Yaw) && finite(r.Pitch) && r.Yaw >= -math.Pi && r.Yaw < math.Pi && r.Pitch >= -math.Pi/2 && r.Pitch <= math.Pi/2
}

// Sweep checks the complete path of a floor-bound cylinder, including its endpoints.
// The XZ distance uses rounded rectangle corners rather than an expanded AABB.
func Sweep(r Room, from, to player.Vector) bool {
	if !ValidPose(r, player.State{Position: from}) || !ValidPose(r, player.State{Position: to}) {
		return false
	}
	for _, b := range r.Surfaces {
		if !b.Collidable || b.Position.Y+b.Size.Y/2 <= 0 || b.Position.Y-b.Size.Y/2 >= 1.8 {
			continue
		}
		if segmentRectangleDistanceSquared(from, to, b) < .3*.3 {
			return false
		}
	}
	return true
}

// segmentRectangleDistanceSquared minimizes the piecewise quadratic distance.
// A coordinate contributes only while it is outside the rectangle. Its boundary
// crossings divide the path into intervals with a fixed quadratic expression.
func segmentRectangleDistanceSquared(from, to player.Vector, b Surface) float64 {
	var cuts [6]float64
	cuts[0], cuts[1] = 0, 1
	n := 2
	axes := [2][4]float64{{from.X, to.X - from.X, b.Position.X - b.Size.X/2, b.Position.X + b.Size.X/2}, {from.Z, to.Z - from.Z, b.Position.Z - b.Size.Z/2, b.Position.Z + b.Size.Z/2}}
	for _, axis := range axes {
		if axis[1] == 0 {
			continue
		}
		for _, bound := range axis[2:] {
			t := (bound - axis[0]) / axis[1]
			if t > 0 && t < 1 {
				cuts[n] = t
				n++
			}
		}
	}
	slices.Sort(cuts[:n])
	distance := func(t float64) float64 {
		sum := 0.0
		for _, axis := range axes {
			p := axis[0] + t*axis[1]
			d := math.Max(math.Max(axis[2]-p, p-axis[3]), 0)
			sum += d * d
		}
		return sum
	}
	best := math.Min(distance(0), distance(1))
	for i := 1; i < n; i++ {
		lo, hi := cuts[i-1], cuts[i]
		mid := (lo + hi) / 2
		aa, ab := 0.0, 0.0
		for _, axis := range axes {
			p := axis[0] + mid*axis[1]
			bound := axis[2]
			if p >= axis[2] && p <= axis[3] {
				continue
			}
			if p > axis[3] {
				bound = axis[3]
			}
			aa += axis[1] * axis[1]
			ab += axis[1] * (axis[0] - bound)
		}
		best = math.Min(best, distance(lo))
		if aa > 0 {
			t := math.Max(lo, math.Min(hi, -ab/aa))
			best = math.Min(best, distance(t))
		}
	}
	return best
}

// Visible reports whether a segment avoids collidable block interiors.
// Tangency, including a door endpoint on the floor, does not obscure sight.
func Visible(r Room, from, to player.Vector) bool {
	if !vectorFinite(from) || !vectorFinite(to) {
		return false
	}
	for _, b := range r.Surfaces {
		if b.Collidable && segmentBlockInterior(from, to, b) {
			return false
		}
	}
	return true
}

func segmentBlockInterior(from, to player.Vector, b Surface) bool {
	axes := [3][4]float64{
		{from.X, to.X - from.X, b.Position.X - b.Size.X/2, b.Position.X + b.Size.X/2},
		{from.Y, to.Y - from.Y, b.Position.Y - b.Size.Y/2, b.Position.Y + b.Size.Y/2},
		{from.Z, to.Z - from.Z, b.Position.Z - b.Size.Z/2, b.Position.Z + b.Size.Z/2},
	}
	lo, hi := 0.0, 1.0
	for _, axis := range axes {
		if axis[1] == 0 {
			if axis[0] <= axis[2] || axis[0] >= axis[3] {
				return false
			}
			continue
		}
		a, c := (axis[2]-axis[0])/axis[1], (axis[3]-axis[0])/axis[1]
		lo = math.Max(lo, math.Min(a, c))
		hi = math.Min(hi, math.Max(a, c))
		if lo >= hi {
			return false
		}
	}
	return lo < hi
}
