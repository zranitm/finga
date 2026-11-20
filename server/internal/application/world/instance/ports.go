package instance

import (
	"context"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// Sink is where a map sends a player's events. Send must not block; false drops the player.
type Sink interface {
	Send(model.Event) bool
	// Stream is what the sink sends on; a tick's frame goes once to each stream, for all its players.
	Stream() Stream
}

// Stream carries the frames of every player on it. SendFrame must not block; false drops every recipient.
type Stream interface {
	SendFrame(f *model.Frame, to []model.Recipient) bool
}

// Checkpoints takes a map's checkpoints to save; Put must not block or lose one, though a newer one may replace it.
type Checkpoints interface {
	Put(cp model.Checkpoint)
}

// Dest is where a hand-off goes: a map's, a region's or a match's instance by its id. In a region it's channel Channel when that's open with room, else the World's pick; a switch lands in Channel past its cap, as it checked the room when it began.
type Dest struct {
	ID      string
	Channel int // a region's; 0, the World picks
	Switch  bool
}

// Mover moves a player to another instance; it must not block the calling tick.
type Mover interface {
	Transfer(from *Instance, p model.Player, sink Sink, dest Dest)
	// Dropped tells of a player the map removed because its sink failed.
	Dropped(from *Instance, id kernel.CharacterID, conn string)
}

// Equipment is the item module's equip and unequip, called from the item-call pool, never inside a tick; a failure that is no rule is transient.ErrUnavailable.
type Equipment interface {
	Equip(ctx context.Context, c model.EquipCall) (model.Equipped, error)
	Unequip(ctx context.Context, c model.UnequipCall) (model.Equipped, error)
}

// HandIns is the item module's hand-in, called from the item-call pool, never inside a tick; a failure that is no rule is transient.ErrUnavailable.
type HandIns interface {
	HandIn(ctx context.Context, c model.HandInCall) (model.Inventory, error)
}

// Gifts is the item module's gift of one item under its own key, called from the item-call pool, never inside a tick; a failure that is no rule is transient.ErrUnavailable.
type Gifts interface {
	Give(ctx context.Context, c model.GiftCall) (model.Gift, error)
}

// Looks is the item module's look, called from the item-call pool, never inside a tick; a failure that is no rule is transient.ErrUnavailable.
type Looks interface {
	SetLook(ctx context.Context, c model.LookCall) (model.Look, error)
}

// Potions is the item module's use of an item, called from the item-call pool, never inside a tick; a failure that is no rule is transient.ErrUnavailable.
// replayed is true when an earlier call under the key took the item.
type Potions interface {
	Use(ctx context.Context, c model.UseCall) (inv model.Inventory, replayed bool, err error)
}

// Results takes a match's outcome; Ended must not block.
type Results interface {
	Ended(r model.MatchResult)
}

// TickObserver sees each tick's duration against its budget.
type TickObserver interface {
	ObserveTick(mapID string, channel int, took, budget time.Duration)
}

// Parties answers party questions from inside a tick, and takes what the map saw of a member.
type Parties interface {
	// Party is a character's party, the zero one without.
	Party(id kernel.CharacterID) model.Party
	Update(u model.MemberUpdate)
	// Epoch changes when the party stream subscribes again, after which the map tells every player's state again.
	Epoch() uint64
}
