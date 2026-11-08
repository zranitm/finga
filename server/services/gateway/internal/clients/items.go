package clients

import (
	"context"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// Items is the application's Items over the item service.
type Items struct{ c itemv1.ItemServiceClient }

// NewItems adapts the item client.
func NewItems(c itemv1.ItemServiceClient) Items { return Items{c: c} }

// Inventory is the items in the character's tabs, an empty slice when it has none, and what it wears, at their revision.
func (i Items) Inventory(ctx context.Context, character kernel.CharacterID) (model.Inventory, error) {
	res, err := i.c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: character.String()})
	if err != nil {
		return model.Inventory{}, errmap.Wrap(err)
	}
	inv := model.Inventory{Revision: res.GetRevision(), Items: make([]model.ItemView, 0, len(res.GetItems()))}
	for _, it := range res.GetItems() {
		view := itemView(it)
		if slot := it.GetEquipSlot(); slot != "" {
			inv.Equipment = append(inv.Equipment, model.EquipSlot{Slot: slot, Item: &view})
			continue
		}
		inv.Items = append(inv.Items, view)
	}
	return inv, nil
}

// Move moves an item and returns the slots it changed.
func (i Items) Move(ctx context.Context, a model.Actor, in model.ItemMove) (model.InventoryChanged, error) {
	res, err := i.c.Move(ctx, &itemv1.MoveRequest{Actor: actor(a), IdempotencyKey: in.IdempotencyKey, ItemId: in.Item.String(), Tab: in.Tab, Slot: int32(in.Slot)})
	if err != nil {
		return model.InventoryChanged{}, errmap.Wrap(err)
	}
	return model.InventoryChanged{Revision: res.GetRevision(), Slots: inventorySlots(res.GetChanged())}, nil
}

// Discard discards an item, or part of a stack, and returns the slots it changed.
func (i Items) Discard(ctx context.Context, a model.Actor, in model.ItemDiscard) (model.InventoryChanged, error) {
	res, err := i.c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor(a), IdempotencyKey: in.IdempotencyKey, ItemId: in.Item.String(), Quantity: int32(in.Quantity)})
	if err != nil {
		return model.InventoryChanged{}, errmap.Wrap(err)
	}
	return model.InventoryChanged{Revision: res.GetRevision(), Slots: inventorySlots(res.GetChanged())}, nil
}

func actor(a model.Actor) *itemv1.Actor {
	return &itemv1.Actor{AccountId: a.Account.String(), CharacterId: a.Character.String()}
}

func itemView(it *itemv1.Item) model.ItemView {
	view := model.ItemView{ID: it.GetId(), Template: it.GetTemplateId(), Tab: it.GetTab(), Slot: int(it.GetSlot()), Quantity: int(it.GetQuantity())}
	if s := it.GetStats(); s != nil {
		view.Stats = rollOf(s)
	}
	return view
}

func inventorySlots(slots []*itemv1.Slot) []model.InventorySlot {
	out := make([]model.InventorySlot, 0, len(slots))
	for _, s := range slots {
		slot := model.InventorySlot{Tab: s.GetTab(), Slot: int(s.GetSlot())}
		if s.GetItem() != nil {
			item := itemView(s.GetItem())
			slot.Item = &item
		}
		out = append(out, slot)
	}
	return out
}
