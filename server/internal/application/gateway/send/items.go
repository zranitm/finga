package send

import (
	"context"

	"finga/internal/application/gateway/session"
	model "finga/internal/model/gateway"
)

// MoveItem moves an item and returns the slots it changed.
func (g *Gateway) MoveItem(ctx context.Context, s *session.Session, in model.ItemMove) (model.InventoryChanged, error) {
	return g.deps.Items.Move(ctx, s.Actor(), in)
}

// Equip wears an item through the world, which checks the live level and applies the new stats; the changes come on its stream, before or after the answer.
func (g *Gateway) Equip(ctx context.Context, s *session.Session, in model.ItemEquip) error {
	return g.deps.World.Equip(ctx, s.Presence(), in)
}

// Unequip takes a worn item off through the world; the changes come on its stream, before or after the answer.
func (g *Gateway) Unequip(ctx context.Context, s *session.Session, in model.ItemUnequip) error {
	return g.deps.World.Unequip(ctx, s.Presence(), in)
}

// UseItem drinks a potion through the world, which checks there is something to restore; the changes come on its stream, before or after the answer.
func (g *Gateway) UseItem(ctx context.Context, s *session.Session, in model.ItemUse) error {
	return g.deps.World.UseItem(ctx, s.Presence(), in)
}

// DiscardItem destroys some or all of a stack and returns the slots it changed.
func (g *Gateway) DiscardItem(ctx context.Context, s *session.Session, in model.ItemDiscard) (model.InventoryChanged, error) {
	return g.deps.Items.Discard(ctx, s.Actor(), in)
}

// DiscardItems destroys whole items or stacks, all of them or none, and returns the slots it changed.
func (g *Gateway) DiscardItems(ctx context.Context, s *session.Session, in model.ItemDiscardMany) (model.InventoryChanged, error) {
	return g.deps.Items.DiscardMany(ctx, s.Actor(), in)
}

// AddToWardrobe takes one of an item that holds a piece and adds the piece to the account's wardrobe, returning the slot it changed.
func (g *Gateway) AddToWardrobe(ctx context.Context, s *session.Session, in model.WardrobeAdd) (model.InventoryChanged, error) {
	return g.deps.Items.AddToWardrobe(ctx, s.Actor(), in)
}

// TakeLoot takes picked items from the player's own pouches through the world, answering each pick: taken, or refused with why.
func (g *Gateway) TakeLoot(ctx context.Context, s *session.Session, picks []model.LootPick) ([]model.PickAnswer, error) {
	return g.deps.World.TakeLoot(ctx, s.Presence(), picks)
}

// SetLook wears a look through the world, which refuses a dead player or one in combat; every view carries it once applied.
func (g *Gateway) SetLook(ctx context.Context, s *session.Session, in model.LookSet) error {
	return g.deps.World.SetLook(ctx, s.Presence(), in)
}
