package account

import (
	"context"
	"testing"

	"finga/internal/application/item"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
)

// itemModule records the last call and answers with inv.
type itemModule struct {
	req any
	inv item.Inventory
}

func (m *itemModule) Inventory(_ context.Context, owner kernel.CharacterID) (item.Inventory, error) {
	m.req = owner
	return m.inv, nil
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

// itemRenames are an item's fields under their view's names, below prefix.
func itemRenames(from, to string) map[string]string {
	return map[string]string{from: to, from + ".TemplateID": to + ".Template"}
}
