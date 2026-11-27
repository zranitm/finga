package item

import (
	"testing"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/pkg/copytest"
)

// The slots a move or a discard changed reach the client with every field: each item with its roll.
func TestTheSlotsACommandChangedKeepEveryField(t *testing.T) {
	renames := itemRenames("Changed.Item", "Slots.Item")
	renames["Changed"] = "Slots"
	match := copytest.Match{
		Renames: renames,
		Skip: map[string]string{
			"Changed.Item.EquipSlot": "an item in a tab is worn nowhere",
			"Equipment":              "a move or a discard changes no worn item",
			"Worn":                   "a move or a discard changes no worn item",
			"Piece":                  "an add's piece comes beside its slots",
			"Pieces":                 "only a hand-in, which the world asks for, gives pieces",
			"Look":                   "a move or a discard changes no look",
		},
	}
	if got := copytest.Diff[item.Result, *clientv1.InventoryChanged](match, inventoryChanged); len(got) > 0 {
		t.Error(got)
	}
}

// itemRenames are an item's fields under the wire's names, from one path to another.
func itemRenames(from, to string) map[string]string {
	renames := map[string]string{from: to, from + ".TemplateID": to + ".Template", from + ".ID": to + ".ItemId"}
	for f, w := range map[string]string{"STR": "Str", "DEX": "Dex", "INT": "Int", "AGI": "Agi", "VIT": "Vit", "MaxHP": "MaxHp", "HPRegen": "HpRegen"} {
		renames[from+".Stats."+f] = to + ".Stats." + w
	}
	return renames
}

// An inventory reaches the client in its Welcome with every field of every item, its roll too: those in a tab as items, the worn ones by their equip slot.
func TestAnInventoryKeepsItsFields(t *testing.T) {
	inventory := func(wearing bool) func(item.Inventory) []*clientv1.ItemView {
		return func(inv item.Inventory) []*clientv1.ItemView {
			for i := range inv.Items {
				if !wearing {
					inv.Items[i].EquipSlot = ""
				}
			}
			if !wearing {
				return inventoryItems(inv)
			}
			var out []*clientv1.ItemView
			for _, s := range worn(inv) {
				out = append(out, s.Item)
			}
			return out
		}
	}
	tabs := copytest.Match{Renames: itemRenames("Items", "")}
	tabs.Skip = map[string]string{"Revision": "the revision is the Welcome's own: TestAnInventoryKeepsItsRevision"}
	if got := copytest.Diff(tabs, inventory(false)); len(got) > 0 {
		t.Errorf("in tabs: %v", got)
	}
	wornM := copytest.Match{Renames: itemRenames("Items", "")}
	wornM.Skip = map[string]string{"Revision": "the revision is the Welcome's own: TestAnInventoryKeepsItsRevision"}
	wornM.Skip["Items.EquipSlot"] = "it names the equip slot, which the slot carries"
	if got := copytest.Diff(wornM, inventory(true)); len(got) > 0 {
		t.Errorf("worn: %v", got)
	}
}

// The piece an add to the wardrobe made is told as the item's: its ID, and an item as what gave it.
func TestAnAddedPieceIsToldAsTheItems(t *testing.T) {
	got := pieceAdded(piecesAdded{Piece: "knight_plate"})
	if len(got.Pieces) != 1 || got.Pieces[0].Id != "knight_plate" || got.Pieces[0].Cause != item.PieceFromItem {
		t.Fatalf("told %+v", got)
	}
}
