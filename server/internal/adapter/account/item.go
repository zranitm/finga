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
	Move(ctx context.Context, actor item.Actor, r item.MoveRequest) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, r item.DiscardRequest) (item.Result, error)
	DiscardMany(ctx context.Context, actor item.Actor, r item.DiscardManyRequest) (item.Result, error)
	AddToWardrobe(ctx context.Context, actor item.Actor, r item.WardrobeRequest) (item.Result, error)
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

// rollOf is an item's roll, nil for anything but equipment.
func rollOf(s *progression.ItemStats) *model.ItemStats {
	if s == nil {
		return nil
	}
	return &model.ItemStats{
		STR: s.STR, DEX: s.DEX, INT: s.INT, AGI: s.AGI, VIT: s.VIT,
		PhysicalAttack: s.PhysicalAttack, MagicAttack: s.MagicAttack, PhysicalDefense: s.PhysicalDefense, MagicDefense: s.MagicDefense,
		MaxHP: s.MaxHP, Accuracy: s.Accuracy, Evasion: s.Evasion,
		CritRate: s.CritRate, CritDamage: s.CritDamage, Penetration: s.Penetration, HPRegen: s.HPRegen,
		CooldownSpeed: s.CooldownSpeed, MoveSpeed: s.MoveSpeed,
	}
}
