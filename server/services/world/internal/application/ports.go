package application

import (
	"context"
	"time"

	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

// Characters loads a character for a join; an adapter over the character
// service implements it. A character that is not the account's fails the
// way the character service says (not found).
type Characters interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
}

// Starter grants a character its starter items once; an adapter over the
// item service implements it. The world calls it on every join as the
// safety net for a character created while the grant could not reach the
// item service (page 10).
type Starter interface {
	GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) error
}

// Sink is where an instance sends a player's events. Send never blocks: the
// gateway enqueues on the connection and returns false when the queue is
// full, and the instance then drops that player, never the tick.
type Sink interface {
	Send(model.Event) bool
}

// Saver persists checkpoints and level-ups; an adapter over the character
// service implements it. It is called from the persistence worker, never
// from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUpSave) error
	// Retryable reports whether a failed save may succeed when tried again:
	// a transient failure, not a rule it ran into.
	Retryable(err error) bool
}

// Mover moves a player to another map's instance after a respawn or a
// portal; the World implements it. It must not block the calling tick.
type Mover interface {
	Transfer(p model.Player, sink Sink, mapID string)
}

// TickObserver sees each tick's duration against its budget;
// internal/telemetry implements it.
type TickObserver interface {
	ObserveTick(mapID string, took, budget time.Duration)
}

// Pickups is what the world needs from the item service; an adapter over
// its client implements it. Called from economy workers, never inside a
// tick.
type Pickups interface {
	// Add puts a drop in the character's inventory and returns the slots it
	// changed.
	Add(ctx context.Context, p model.Pickup) ([]model.Slot, error)
	// Retryable reports whether a failed Add may succeed when tried again
	// with the same drop: a transient failure, not a rule the pickup ran
	// into (a full inventory).
	Retryable(err error) bool
}
