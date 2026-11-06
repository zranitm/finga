package send

import (
	"context"

	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// MoveItem moves an item and returns the slots it changed.
func (g *Gateway) MoveItem(ctx context.Context, s *session.Session, in model.ItemMove) ([]model.InventorySlot, error) {
	return g.deps.Items.Move(ctx, s.Actor(), in)
}

// DiscardItem destroys some or all of a stack and returns the slots it changed.
func (g *Gateway) DiscardItem(ctx context.Context, s *session.Session, in model.ItemDiscard) ([]model.InventorySlot, error) {
	return g.deps.Items.Discard(ctx, s.Actor(), in)
}
