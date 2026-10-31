// Package player defines persisted player positions independently of world storage.
package player

import "time"

// Vector is a point in the world's right-handed coordinate system.
type Vector struct{ X, Y, Z float64 }

// Rotation describes the view in radians.
type Rotation struct{ Yaw, Pitch float64 }

// State is a persisted pose. UpdatedAt is its optimistic concurrency revision.
type State struct {
	WorldVersion, RoomID string
	Position             Vector
	Rotation             Rotation
	UpdatedAt            time.Time
}
