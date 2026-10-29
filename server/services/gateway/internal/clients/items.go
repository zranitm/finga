package clients

import (
	"context"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// Items is the application's Items over the item service.
type Items struct{ c itemv1.ItemServiceClient }

// NewItems adapts the item client.
func NewItems(c itemv1.ItemServiceClient) Items { return Items{c: c} }

// Inventory is the character's items, an empty slice when it has none.
func (i Items) Inventory(ctx context.Context, character kernel.CharacterID) ([]protocol.ItemView, error) {
	res, err := i.c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: character.String()})
	if err != nil {
		return nil, errmap.Wrap(err)
	}
	return itemViews(res.GetItems()), nil
}

// Move moves an item and returns the slots it changed.
func (i Items) Move(ctx context.Context, a model.Actor, in protocol.InventoryMove) ([]protocol.InventorySlot, error) {
	res, err := i.c.Move(ctx, &itemv1.MoveRequest{Actor: actor(a), IdempotencyKey: in.IdempotencyKey, ItemId: in.ItemID, Tab: in.Tab, Slot: int32(in.Slot)})
	if err != nil {
		return nil, errmap.Wrap(err)
	}
	return inventorySlots(res.GetChanged()), nil
}

// Discard discards an item, or part of a stack, and returns the slots it
// changed.
func (i Items) Discard(ctx context.Context, a model.Actor, in protocol.InventoryDiscard) ([]protocol.InventorySlot, error) {
	res, err := i.c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor(a), IdempotencyKey: in.IdempotencyKey, ItemId: in.ItemID, Quantity: int32(in.Quantity)})
	if err != nil {
		return nil, errmap.Wrap(err)
	}
	return inventorySlots(res.GetChanged()), nil
}

func actor(a model.Actor) *itemv1.Actor {
	return &itemv1.Actor{AccountId: a.Account.String(), CharacterId: a.Character.String()}
}

func itemViews(items []*itemv1.Item) []protocol.ItemView {
	out := make([]protocol.ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, itemView(it))
	}
	return out
}

func itemView(it *itemv1.Item) protocol.ItemView {
	return protocol.ItemView{ItemID: it.GetId(), Template: it.GetTemplateId(), Tab: it.GetTab(), Slot: int(it.GetSlot()), Quantity: int(it.GetQuantity())}
}

// inventorySlots are the item service's changed slots as the client sees
// them.
func inventorySlots(slots []*itemv1.Slot) []protocol.InventorySlot {
	out := make([]protocol.InventorySlot, 0, len(slots))
	for _, s := range slots {
		slot := protocol.InventorySlot{Tab: s.GetTab(), Slot: int(s.GetSlot())}
		if s.GetItem() != nil {
			v := itemView(s.GetItem())
			slot.Item = &v
		}
		out = append(out, slot)
	}
	return out
}
