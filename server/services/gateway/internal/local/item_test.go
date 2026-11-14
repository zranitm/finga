package local

import (
	"context"
	"testing"

	"finga/internal/kernel"
	"finga/pkg/copytest"
	"finga/services/gateway/internal/model"
	"finga/services/item"
)

// itemModule records the last call and answers with inv or res.
type itemModule struct {
	actor item.Actor
	req   any
	inv   item.Inventory
	res   item.Result
}

func (m *itemModule) Inventory(_ context.Context, owner kernel.CharacterID) (item.Inventory, error) {
	m.req = owner
	return m.inv, nil
}

func (m *itemModule) Move(_ context.Context, a item.Actor, r item.MoveRequest) (item.Result, error) {
	m.actor, m.req = a, r
	return m.res, nil
}

func (m *itemModule) Discard(_ context.Context, a item.Actor, r item.DiscardRequest) (item.Result, error) {
	m.actor, m.req = a, r
	return m.res, nil
}

func (m *itemModule) DiscardMany(_ context.Context, a item.Actor, r item.DiscardManyRequest) (item.Result, error) {
	m.actor, m.req = a, r
	return m.res, nil
}

// asked is what a use case hands an adapter, and sent what the adapter hands the module.
type (
	asked[In any] struct {
		Actor model.Actor
		In    In
	}
	sent[Req any] struct {
		Actor item.Actor
		Req   Req
	}
)

// requestRenames are the item requests' fields under their module names.
var requestRenames = map[string]string{"In": "Req", "In.IdempotencyKey": "Req.Key", "In.Item": "Req.ItemID", "In.Items": "Req.ItemIDs"}

// Each item call reaches the module with every field of its request.
func TestEachItemRequestKeepsItsFields(t *testing.T) {
	m := &itemModule{}
	items := NewItems(m)
	match := copytest.Match{Renames: requestRenames}
	for name, diff := range map[string]func() []string{
		"Move": func() []string {
			return copytest.Diff(match, func(a asked[model.ItemMove]) sent[item.MoveRequest] {
				_, _ = items.Move(t.Context(), a.Actor, a.In)
				return sent[item.MoveRequest]{m.actor, m.req.(item.MoveRequest)}
			})
		},
		"Discard": func() []string {
			return copytest.Diff(match, func(a asked[model.ItemDiscard]) sent[item.DiscardRequest] {
				_, _ = items.Discard(t.Context(), a.Actor, a.In)
				return sent[item.DiscardRequest]{m.actor, m.req.(item.DiscardRequest)}
			})
		},
		"DiscardMany": func() []string {
			return copytest.Diff(match, func(a asked[model.ItemDiscardMany]) sent[item.DiscardManyRequest] {
				_, _ = items.DiscardMany(t.Context(), a.Actor, a.In)
				return sent[item.DiscardManyRequest]{m.actor, m.req.(item.DiscardManyRequest)}
			})
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// itemRenames are an item's fields under their view's names, below prefix.
func itemRenames(from, to string) map[string]string {
	return map[string]string{from: to, from + ".TemplateID": to + ".Template"}
}

// The slots a move or a discard changed reach the use cases with every field: each item with its roll.
func TestEachItemResultKeepsItsFields(t *testing.T) {
	m := &itemModule{}
	items := NewItems(m)
	match := copytest.Match{
		Renames: itemRenames("Changed.Item", "Slots.Item"),
		Skip: map[string]string{
			"Changed.Item.EquipSlot": "an item in a tab is worn nowhere",
			"Equipment":              "a move or a discard changes no worn item",
			"Worn":                   "a move or a discard changes no worn item",
		},
	}
	match.Renames["Changed"] = "Slots"
	for name, call := range map[string]func() model.InventoryChanged{
		"Move": func() model.InventoryChanged {
			out, _ := items.Move(t.Context(), model.Actor{}, model.ItemMove{})
			return out
		},
		"Discard": func() model.InventoryChanged {
			out, _ := items.Discard(t.Context(), model.Actor{}, model.ItemDiscard{})
			return out
		},
		"DiscardMany": func() model.InventoryChanged {
			out, _ := items.DiscardMany(t.Context(), model.Actor{}, model.ItemDiscardMany{})
			return out
		},
	} {
		got := copytest.Diff(match, func(res item.Result) model.InventoryChanged {
			m.res = res
			return call()
		})
		if len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// An inventory reaches the use cases with every field of every item, its roll too: those in a tab as items, the worn ones by their equip slot.
func TestAnInventoryKeepsItsFields(t *testing.T) {
	m := &itemModule{}
	items := NewItems(m)
	owner := kernel.NewCharacterID()
	inventory := func(worn bool) func(item.Inventory) model.Inventory {
		return func(inv item.Inventory) model.Inventory {
			for i := range inv.Items {
				if !worn {
					inv.Items[i].EquipSlot = ""
				}
			}
			m.inv = inv
			out, _ := items.Inventory(t.Context(), owner)
			if m.req != owner {
				t.Errorf("asked for %v's inventory, not %v's", m.req, owner)
			}
			return out
		}
	}
	if got := copytest.Diff(copytest.Match{Renames: itemRenames("Items", "Items")}, inventory(false)); len(got) > 0 {
		t.Errorf("in tabs: %v", got)
	}
	worn := copytest.Match{Renames: itemRenames("Items", "Equipment.Item")}
	worn.Renames["Items.EquipSlot"] = "Equipment.Slot"
	if got := copytest.Diff(worn, inventory(true)); len(got) > 0 {
		t.Errorf("worn: %v", got)
	}
}
