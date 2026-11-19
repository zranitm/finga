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

// Social is the social module as the queue calls it.
type Social interface {
	// PartyOf is the character's party, false without one.
	PartyOf(ctx context.Context, id kernel.CharacterID) (model.Party, bool, error)
	// FollowParties tells changed each party's change, its characters and its members after it, none for out of one, until ctx ends; after each subscription, resync looks again at what a break may have hidden.
	FollowParties(ctx context.Context, changed func(characters, members []kernel.CharacterID), resync func(ctx context.Context))
}

// Store keeps ratings and match history.
type Store interface {
	// Rating is the character's rating in mode, false when they have none yet.
	Rating(ctx context.Context, id kernel.CharacterID, mode string) (model.Rating, bool, error)
	// Record stores match once, giving back each fighter's rating before and after: rate gets the rows of the characters that have one, locked, and gives what the outcome does to them. A repeat stores nothing, and gives back what the first stored, the characters' records as they are now. rate may run more than once.
	Record(ctx context.Context, match model.Match, characters []kernel.CharacterID, rate func(before map[kernel.CharacterID]model.Rating) model.Rated) ([]model.Fighter, error)
}

// Notifier pushes to a player; it never blocks.
type Notifier interface {
	Push(to kernel.CharacterID, d Delivery)
}
