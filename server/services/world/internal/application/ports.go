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

// Saver persists checkpoints, from the persistence worker, never from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	Retryable(err error) bool
}

// Checkpoints takes a map's checkpoints for saving. Put must not block or drop one; a newer checkpoint of a
// character may replace an older one not yet saved.
type Checkpoints interface {
	Put(cp model.Checkpoint)
}

// Mover moves a player to another map's instance; it must not block the calling tick.
type Mover interface {
	Transfer(p model.Player, sink Sink, mapID string)
}

// TickObserver sees each tick's duration against its budget.
type TickObserver interface {
	ObserveTick(mapID string, took, budget time.Duration)
}

// Social is the social service as the party copy sees it.
type Social interface {
	// FollowParties tells on each event of the party stream, subscribed as world, until ctx ends; a
	// broken subscription is PartiesLost, and another follows.
	FollowParties(ctx context.Context, world string, on func(model.PartyEvent))
	// UpdateMember tells the service what a map saw of a member.
	UpdateMember(ctx context.Context, u model.MemberUpdate) error
}

// Pickups is the item service, called from economy workers, never inside a tick.
type Pickups interface {
	Add(ctx context.Context, p model.Pickup) ([]model.Slot, error)
	// Retryable reports whether a failed Add is transient (a full inventory is not).
	Retryable(err error) bool
}
