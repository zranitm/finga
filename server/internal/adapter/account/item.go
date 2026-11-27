package account

import (
	"context"

	"finga/internal/application/item"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// ItemModule is the item module as the gateway calls it.
type ItemModule interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
}

// Items is the application's Items over the item module.
type Items struct{ items ItemModule }

// NewItems adapts the item module.
func NewItems(items ItemModule) Items { return Items{items: items} }

// Inventory is the items in the character's tabs, an empty slice when it has none, and what it wears, at their revision.
func (i Items) Inventory(ctx context.Context, character kernel.CharacterID) (model.Inventory, error) {
	got, err := i.items.Inventory(ctx, character)
	if err != nil {
		return model.Inventory{}, wrap(err)
	}
	inv := model.Inventory{Revision: got.Revision, Items: make([]model.ItemView, 0, len(got.Items))}
	for _, it := range got.Items {
		view := itemView(it)
		if it.EquipSlot != "" {
			inv.Equipment = append(inv.Equipment, model.EquipSlot{Slot: it.EquipSlot, Item: &view})
			continue
		}
		inv.Items = append(inv.Items, view)
	}
	return inv, nil
}

func itemView(it item.Item) model.ItemView {
	return model.ItemView{ID: it.ID.String(), Template: it.TemplateID, Tab: string(it.Tab), Slot: it.Slot, Quantity: it.Quantity, Stats: rollOf(it.Stats)}
}

// rollOf is a copy of an item's roll, nil for anything but equipment.
func rollOf(s *progression.ItemStats) *model.ItemStats {
	if s == nil {
		return nil
	}
	roll := *s
	return &roll
}
