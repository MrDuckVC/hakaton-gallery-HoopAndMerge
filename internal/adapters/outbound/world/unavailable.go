// Package world supplies adapters for validated local world files.
package world

import (
	"context"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
)

// Unavailable represents an explicit unavailable dependency in adapter integration tests.
type Unavailable struct{}

// Start refuses to provide an unverified spawn.
func (Unavailable) Start(context.Context) (player.State, error) {
	return player.State{}, domain.Unavailable
}

// Valid refuses to validate positions without a catalog.
func (Unavailable) Valid(context.Context, player.State) (bool, error) {
	return false, domain.Unavailable
}
