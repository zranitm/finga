package application

import (
	"errors"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/item/internal/model"
)

func TestAnItemGoesToASlotOfItsKind(t *testing.T) {
	slots := []gamedata.EquipSlot{{ID: "weapon", Kind: "weapon"}, {ID: "hat", Kind: "hat"}, {ID: "ring_1", Kind: "ring"}, {ID: "ring_2", Kind: "ring"}}
	wearing := func(ids ...string) []model.Item {
		var inv []model.Item
		for _, id := range ids {
			inv = append(inv, model.Item{ID: kernel.NewItemID(), EquipSlot: id, Quantity: 1})
		}
		return append(inv, model.Item{ID: kernel.NewItemID(), Tab: gamedata.Equip, Slot: 0, Quantity: 1})
	}
	tests := []struct {
		name  string
		kind  gamedata.SlotKind
		asked string
		inv   []model.Item
		want  string
		err   error
	}{
		{"a weapon goes to the weapon slot", "weapon", "", wearing(), "weapon", nil},
		{"onto a worn weapon too, which it swaps out", "weapon", "", wearing("weapon"), "weapon", nil},
		{"a ring takes the first empty ring slot", "ring", "", wearing("ring_1"), "ring_2", nil},
		{"with every ring slot taken, the first", "ring", "", wearing("ring_1", "ring_2"), "ring_1", nil},
		{"with none taken, the first", "ring", "", wearing(), "ring_1", nil},
		{"a ring asked into ring_2 goes there, worn or not", "ring", "ring_2", wearing("ring_2"), "ring_2", nil},
		{"a ring asked into the hat slot", "ring", "hat", wearing(), "", model.ErrWrongEquipSlot},
		{"a slot the data doesn't have", "weapon", "boots", wearing(), "", model.ErrWrongEquipSlot},
		{"a kind no slot takes", "cape", "", wearing(), "", model.ErrWrongEquipSlot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := equipSlotFor(slots, tt.kind, tt.asked, tt.inv)
			if got != tt.want || !errors.Is(err, tt.err) {
				t.Fatalf("got %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}
