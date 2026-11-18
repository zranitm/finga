package service

import (
	"context"

	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// World is the world module as the matcher calls it.
type World interface {
	// CanFight is nil when the character could go into a match now.
	CanFight(ctx context.Context, id kernel.CharacterID) error
	OpenMatch(ctx context.Context, spec model.Spec) error
}

// Store keeps ratings and match history.
type Store interface {
	// Rating is the character's rating in mode, false when they have none yet.
	Rating(ctx context.Context, id kernel.CharacterID, mode string) (model.Rating, bool, error)
	// Record stores match once: rate gets the rows of the characters that have one, locked, and gives what the outcome does to them; false when the match was stored already. rate may run more than once.
	Record(ctx context.Context, match model.Match, characters []kernel.CharacterID, rate func(before map[kernel.CharacterID]model.Rating) model.Rated) (bool, error)
}

// Notifier pushes to a player; it never blocks.
type Notifier interface {
	Push(to kernel.CharacterID, d Delivery)
}
