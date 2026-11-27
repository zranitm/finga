package account

import (
	"context"
	"testing"

	"finga/internal/application/item"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
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

// An inventory reaches the Welcome as the item module has it.
func TestAnInventoryIsHandedOnAsTheModuleHasIt(t *testing.T) {
	m := &itemModule{inv: item.Inventory{Revision: 7, Items: []item.Item{{TemplateID: "red_potion", Quantity: 3}}}}
	owner := kernel.NewCharacterID()
	got, err := NewItems(m).Inventory(t.Context(), owner)
	inv, ok := got.(item.Inventory)
	if err != nil || !ok || inv.Revision != 7 || len(inv.Items) != 1 || inv.Items[0].TemplateID != "red_potion" || m.req != owner {
		t.Fatalf("got %+v (%v), asked for %v", got, err, m.req)
	}
}

// The rolls of an inventory handed on are copies, so the Welcome holds none of the module's.
func TestAnInventorysRollsAreCopied(t *testing.T) {
	roll := &progression.ItemStats{STR: 3}
	m := &itemModule{inv: item.Inventory{Items: []item.Item{{TemplateID: "short_bow", Stats: roll}}}}
	got, err := NewItems(m).Inventory(t.Context(), kernel.NewCharacterID())
	if err != nil {
		t.Fatal(err)
	}
	inv := got.(item.Inventory)
	if inv.Items[0].Stats == roll || *inv.Items[0].Stats != *roll {
		t.Fatalf("the roll went on as %p %+v, the module's is %p %+v", inv.Items[0].Stats, inv.Items[0].Stats, roll, roll)
	}
	roll.STR = 9
	if inv.Items[0].Stats.STR != 3 {
		t.Fatal("the module's roll changed under the Welcome's")
	}
	if m.inv.Items[0].Stats != roll {
		t.Fatal("the module's own inventory was rewritten")
	}
}
