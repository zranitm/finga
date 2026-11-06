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

// Saver persists checkpoints, from the persistence worker, never from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	Retryable(err error) bool
}
