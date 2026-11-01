package application

import (
	"context"
	"time"

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

// Sink is where an instance sends a player's events. Send must not block; false drops the player.
type Sink interface {
	Send(model.Event) bool
}

// Saver persists checkpoints and level-ups, from the persistence worker, never from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUpSave) error
	Retryable(err error) bool
}

// Mover moves a player to another map's instance; it must not block the calling tick.
type Mover interface {
	Transfer(p model.Player, sink Sink, mapID string)
}

// TickObserver sees each tick's duration against its budget.
type TickObserver interface {
	ObserveTick(mapID string, took, budget time.Duration)
}

// Pickups is the item service, called from economy workers, never inside a tick.
type Pickups interface {
	Add(ctx context.Context, p model.Pickup) ([]model.Slot, error)
	// Retryable reports whether a failed Add is transient (a full inventory is not).
	Retryable(err error) bool
}
