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
	// OpenMatch opens spec's match, which knows its players once it returns, its bots counted in Bots from then on.
	OpenMatch(ctx context.Context, spec model.Spec) error
	// Bots is how many bots the matches running have, as their specs placed them.
	Bots() int
	// SetBuild hands the character's match their arena build, for its next round; nothing when they're in none.
	SetBuild(ctx context.Context, id kernel.CharacterID, b model.Build) error
}

// Characters is the character module as pvp reads it.
type Characters interface {
	// Character is a character of any account; model.ErrNoCharacter for none.
	Character(ctx context.Context, id kernel.CharacterID) (model.Character, error)
	// Names are the characters by id.
	Names(ctx context.Context, ids []kernel.CharacterID) (map[kernel.CharacterID]model.Named, error)
}

// Social is the social module as the queue calls it.
type Social interface {
	// PartyOf is the character's party, false without one.
	PartyOf(ctx context.Context, id kernel.CharacterID) (model.Party, bool, error)
	// FollowParties tells changed each party's change, its characters and its members after it, none for out of one, until ctx ends; after each subscription, resync looks again at what a break may have hidden.
	FollowParties(ctx context.Context, changed func(characters, members []kernel.CharacterID), resync func(ctx context.Context))
}

// Store keeps ratings, match history and arena builds.
type Store interface {
	// Rating is the character's rating in mode, false when they have none yet.
	Rating(ctx context.Context, id kernel.CharacterID, mode string) (model.Rating, bool, error)
	// Record stores match once, giving back each fighter's rating before and after: rate gets the rows of the characters that have one, locked, and gives what the outcome does to them. A repeat stores nothing, and gives back what the first stored, the characters' records as they are now. rate may run more than once.
	Record(ctx context.Context, match model.Match, characters []kernel.CharacterID, rate func(before map[kernel.CharacterID]model.Rating) model.Rated) ([]model.Fighter, error)
	// Ratings are the character's ratings by mode, those they have.
	Ratings(ctx context.Context, id kernel.CharacterID) (map[string]model.Rating, error)
	// Board is the most placed ratings of mode, highest first.
	Board(ctx context.Context, mode string, most int) ([]model.Ranked, error)
	// Above counts the placed ratings of mode higher than shown.
	Above(ctx context.Context, mode string, shown int) (int, error)
	// Build is the character's saved arena build, false when they've saved none.
	Build(ctx context.Context, id kernel.CharacterID) (model.Build, bool, error)
	// Untold is each stored match the character was away from at its end and hasn't been told of.
	Untold(ctx context.Context, id kernel.CharacterID) ([]model.Untold, error)
	// MarkTold marks the character told of matches.
	MarkTold(ctx context.Context, id kernel.CharacterID, matches []kernel.MatchID) error
	// PutBuild saves the character's arena build, giving back its revision.
	PutBuild(ctx context.Context, id kernel.CharacterID, b model.Build) (int, error)
}

// Items is the item module as pvp grants through it.
type Items interface {
	// GrantRankPieces puts each rank's piece in the account's wardrobe, giving back those new to it; a piece it has stays as it was.
	GrantRankPieces(ctx context.Context, account kernel.AccountID, pieces []model.RankPiece) ([]model.RankPiece, error)
}

// Notifier pushes to a player; it never blocks.
type Notifier interface {
	Push(to kernel.CharacterID, d Delivery)
}
