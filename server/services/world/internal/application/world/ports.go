package world

import (
	"context"

	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

// Characters loads an account's character for a join.
type Characters interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
}

// Starter grants starter items once; the world calls it on every join as a safety net.
type Starter interface {
	GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) error
}

// Wardrobe reads what a character wears; a join can't derive its stats without it, so it fails while the read does.
type Wardrobe interface {
	Worn(ctx context.Context, id kernel.CharacterID) (model.Worn, error)
}

// Saver persists checkpoints, from the persistence worker, never from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	// Retryable reports whether a failed save may pass if made again, such as one the service was down for.
	Retryable(err error) bool
	// Refused reports whether the service refused the save by a rule, which no retry passes.
	Refused(err error) bool
}

// The reasons a save is dropped, as SaveObserver hears them.
const (
	DropRefused     = "refused"     // by a rule of the character service
	DropUnexplained = "unexplained" // failing without a reason for unexplainedFor
	DropShutdown    = "shutdown"    // its last try at shutdown failed
)

// DropReasons are all the reasons a save is dropped, so a counter can start each at 0.
func DropReasons() []string { return []string{DropRefused, DropUnexplained, DropShutdown} }

// SaveObserver counts the saves the World gives up on, and so the progress lost.
type SaveObserver interface {
	SaveDropped(reason string)
}
