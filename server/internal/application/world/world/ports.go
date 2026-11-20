package world

import (
	"context"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// Characters loads an account's character for a join.
type Characters interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
}

// Starter grants starter items once; the world calls it on every join as a safety net.
type Starter interface {
	GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) error
}

// Wardrobe reads what a character wears and the look it shows; a join can't derive its stats without them, so it fails while a read does.
type Wardrobe interface {
	Worn(ctx context.Context, id kernel.CharacterID) (model.Worn, error)
	Look(ctx context.Context, id kernel.CharacterID) (model.Look, error)
}

// HandedIns finds which completions the item module handed in; a join fails while it can't, as a hand-in the world missed would be lost.
type HandedIns interface {
	HandedIn(ctx context.Context, id kernel.CharacterID, asked []model.Completion) ([]model.HandedIn, error)
}

// Saver persists checkpoints, from the persistence worker, never from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	// Retryable reports whether a failed save may pass if made again, such as one the character module was down for.
	Retryable(err error) bool
	// Refused reports whether the character module refused the save by a rule, which no retry passes.
	Refused(err error) bool
}

// The reasons a save is dropped, as SaveObserver hears them.
const (
	DropRefused     = "refused"     // by a rule of the character module
	DropUnexplained = "unexplained" // failing without a reason for unexplainedFor
	DropShutdown    = "shutdown"    // its last try at shutdown failed
)

// DropReasons are all the reasons a save is dropped, so a counter can start each at 0.
func DropReasons() []string { return []string{DropRefused, DropUnexplained, DropShutdown} }

// SaveObserver counts the saves the World gives up on, and so the progress lost.
type SaveObserver interface {
	SaveDropped(reason string)
}

// ChannelObserver hears how many channels a region has open, as one opens or closes; it's called under the channels' lock, so it must not block.
type ChannelObserver interface {
	ChannelsOpen(region string, open int)
}
