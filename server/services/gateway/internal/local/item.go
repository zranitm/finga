package local

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/gateway/internal/model"
	"finga/services/item"
)

// ItemModule is the item module as the gateway calls it.
type ItemModule interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
	Move(ctx context.Context, actor item.Actor, r item.MoveRequest) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, r item.DiscardRequest) (item.Result, error)
	DiscardMany(ctx context.Context, actor item.Actor, r item.DiscardManyRequest) (item.Result, error)
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

// Move moves an item and returns the slots it changed.
func (i Items) Move(ctx context.Context, a model.Actor, in model.ItemMove) (model.InventoryChanged, error) {
	res, err := i.items.Move(ctx, actor(a), item.MoveRequest{Key: in.IdempotencyKey, ItemID: in.Item, Tab: gamedata.Tab(in.Tab), Slot: in.Slot})
	if err != nil {
		return model.InventoryChanged{}, wrap(err)
	}
	return changed(res), nil
}

// Discard discards an item, or part of a stack, and returns the slots it changed.
func (i Items) Discard(ctx context.Context, a model.Actor, in model.ItemDiscard) (model.InventoryChanged, error) {
	res, err := i.items.Discard(ctx, actor(a), item.DiscardRequest{Key: in.IdempotencyKey, ItemID: in.Item, Quantity: in.Quantity})
	if err != nil {
		return model.InventoryChanged{}, wrap(err)
	}
	return changed(res), nil
}

// DiscardMany discards whole items or stacks, all of them or none, and returns the slots it changed.
func (i Items) DiscardMany(ctx context.Context, a model.Actor, in model.ItemDiscardMany) (model.InventoryChanged, error) {
	res, err := i.items.DiscardMany(ctx, actor(a), item.DiscardManyRequest{Key: in.IdempotencyKey, ItemIDs: in.Items})
	if err != nil {
		return model.InventoryChanged{}, wrap(err)
	}
	return changed(res), nil
}

func actor(a model.Actor) item.Actor { return item.Actor{Account: a.Account, Character: a.Character} }

func changed(res item.Result) model.InventoryChanged {
	out := model.InventoryChanged{Revision: res.Revision, Slots: make([]model.InventorySlot, 0, len(res.Changed))}
	for _, s := range res.Changed {
		slot := model.InventorySlot{Tab: string(s.Tab), Slot: s.Slot}
		if s.Item != nil {
			view := itemView(*s.Item)
			slot.Item = &view
		}
		out.Slots = append(out.Slots, slot)
	}
	return out
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
