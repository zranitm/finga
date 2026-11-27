package item

import (
	"testing"

	"finga/pkg/copytest"
)

// The slots a move or a discard changed reach the client with every field: each item with its roll.
func TestTheSlotsACommandChangedKeepEveryField(t *testing.T) {
	renames := map[string]string{"Changed": "Slots", "Changed.Item": "Slots.Item", "Changed.Item.TemplateID": "Slots.Item.Template", "Changed.Item.ID": "Slots.Item.ItemId"}
	for from, to := range map[string]string{"STR": "Str", "DEX": "Dex", "INT": "Int", "AGI": "Agi", "VIT": "Vit", "MaxHP": "MaxHp", "HPRegen": "HpRegen"} {
		renames["Changed.Item.Stats."+from] = "Slots.Item.Stats." + to
	}
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
	if got := copytest.Diff(match, inventoryChanged); len(got) > 0 {
		t.Error(got)
	}
}
