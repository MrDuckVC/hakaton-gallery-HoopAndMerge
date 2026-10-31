// Package world supplies catalog adapters; B03 will provide the validated file catalog.
package world

import (
	"context"

	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
)

// Unavailable explicitly refuses to represent test fixtures as a validated production world.
type Unavailable struct{}

// Start cannot provide a verified spawn before the real catalog is connected.
func (Unavailable) Start(context.Context) (player.State, error) {
	return player.State{}, domain.Unavailable
}

// Valid cannot validate stored positions before the real catalog is connected.
func (Unavailable) Valid(context.Context, player.State) (bool, error) {
	return false, domain.Unavailable
}
