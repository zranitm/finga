package pvp

import (
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// Push is something the module tells a player.
type Push struct {
	To       kernel.CharacterID
	Delivery Delivery
}

// Player is a character as they queue.
type Player = model.Player

// FeatPiece is a wardrobe piece a feat gives.
type FeatPiece = model.FeatPiece

// RankPiece is the wardrobe piece a rank gives.
type RankPiece = model.RankPiece

// Build is a character's arena build.
type Build = model.Build
