package send

import (
	"context"

	"finga/modules/gateway/internal/application/session"
	"finga/modules/gateway/internal/model"
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
