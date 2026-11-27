package account

import (
	"context"
	"testing"

	"finga/internal/application/item"
	"finga/internal/shared/kernel"
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
