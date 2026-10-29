package application

import (
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/item/internal/model"
)

func TestAFullTabHasNoFreeSlot(t *testing.T) {
	owner := kernel.NewCharacterID()
	var inv []model.Item
	for slot := range model.SlotsPerTab {
		inv = append(inv, model.Item{ID: kernel.NewItemID(), TemplateID: "slime_gel", Owner: owner, Tab: gamedata.Etc, Slot: slot, Quantity: 100})
	}
	if _, ok := freeSlot(inv, gamedata.Etc); ok {
		t.Fatal("a full tab reported a free slot")
	}
	if slot, ok := freeSlot(inv, gamedata.Use); !ok || slot != 0 {
		t.Fatalf("another tab: %d, %v", slot, ok)
	}
}
