package account

import (
	"context"
	"time"

	model "finga/internal/model/account"
	"finga/pkg/transient"
)

// itemCallFor is all a call to the item module for a command may take: inside the 5 s the gateway gives a command, so the command answers unavailable rather than failing at that deadline.
const itemCallFor = 3500 * time.Millisecond

// MoveItem moves an item and returns the slots it changed.
func (g *Commands) MoveItem(ctx context.Context, s *Connection, in model.ItemMove) (model.InventoryChanged, error) {
	return transient.Within(ctx, itemCallFor, "the item module, asked for a move", func(ctx context.Context) (model.InventoryChanged, error) {
		return g.deps.Items.Move(ctx, s.Actor(), in)
	})
}

// Equip wears an item through the world, which checks the live level and applies the new stats; the changes come on its stream, before or after the answer.
func (g *Commands) Equip(ctx context.Context, s *Connection, in model.ItemEquip) error {
	return g.deps.World.Equip(ctx, s.Presence(), in)
}

// Unequip takes a worn item off through the world; the changes come on its stream, before or after the answer.
func (g *Commands) Unequip(ctx context.Context, s *Connection, in model.ItemUnequip) error {
	return g.deps.World.Unequip(ctx, s.Presence(), in)
}

// UseItem drinks a potion through the world, which checks there is something to restore; the changes come on its stream, before or after the answer.
func (g *Commands) UseItem(ctx context.Context, s *Connection, in model.ItemUse) error {
	return g.deps.World.UseItem(ctx, s.Presence(), in)
}

// DiscardItem destroys some or all of a stack and returns the slots it changed.
func (g *Commands) DiscardItem(ctx context.Context, s *Connection, in model.ItemDiscard) (model.InventoryChanged, error) {
	return transient.Within(ctx, itemCallFor, "the item module, asked for a discard", func(ctx context.Context) (model.InventoryChanged, error) {
		return g.deps.Items.Discard(ctx, s.Actor(), in)
	})
}

// DiscardItems destroys whole items or stacks, all of them or none, and returns the slots it changed.
func (g *Commands) DiscardItems(ctx context.Context, s *Connection, in model.ItemDiscardMany) (model.InventoryChanged, error) {
	return transient.Within(ctx, itemCallFor, "the item module, asked for a discard", func(ctx context.Context) (model.InventoryChanged, error) {
		return g.deps.Items.DiscardMany(ctx, s.Actor(), in)
	})
}

// AddToWardrobe takes one of an item that holds a piece and adds the piece to the account's wardrobe, returning the slot it changed; the session hears PiecesAdded, as a grant's would. A replayed add tells of its piece again, which the client knows.
func (g *Commands) AddToWardrobe(ctx context.Context, s *Connection, in model.WardrobeAdd) (model.InventoryChanged, error) {
	added, err := transient.Within(ctx, itemCallFor, "the item module, asked for a wardrobe add", func(ctx context.Context) (model.WardrobeAdded, error) {
		return g.deps.Items.AddToWardrobe(ctx, s.Actor(), in)
	})
	if err != nil {
		return model.InventoryChanged{}, err
	}
	s.Deliver(model.Delivery{Push: model.PiecesAdded{Pieces: []model.AddedPiece{added.Piece}}})
	return added.Changed, nil
}

// TakeLoot takes picked items from the player's own pouches through the world, answering each pick: taken, or refused with why.
func (g *Commands) TakeLoot(ctx context.Context, s *Connection, picks []model.LootPick) ([]model.PickAnswer, error) {
	return g.deps.World.TakeLoot(ctx, s.Presence(), picks)
}

// SetLook wears a look through the world, which refuses a dead player or one in combat; every view carries it once applied.
func (g *Commands) SetLook(ctx context.Context, s *Connection, in model.LookSet) error {
	return g.deps.World.SetLook(ctx, s.Presence(), in)
}
