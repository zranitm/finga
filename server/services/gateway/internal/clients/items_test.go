package clients_test

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/clients"
	"finga/services/gateway/internal/model"
)

// listing is an item service whose Inventory answers items.
type listing struct {
	itemv1.ItemServiceClient
	items []*itemv1.Item
}

func (l listing) Inventory(context.Context, *itemv1.InventoryRequest, ...grpc.CallOption) (*itemv1.InventoryResponse, error) {
	return &itemv1.InventoryResponse{Items: l.items, Revision: 9}, nil
}

func TestAnInventoryIsWhatTheTabsHoldAndWhatIsWorn(t *testing.T) {
	potion := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "red_potion", Tab: "use", Slot: 2, Quantity: 5}
	bow := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "short_bow", Tab: "equip", Slot: 0, Quantity: 1, Stats: &itemv1.ItemStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, Accuracy: 11, Evasion: 12, CritRate: 13, CritDamage: 14, Penetration: 15, HpRegen: 16, CooldownSpeed: 17, MoveSpeed: 18}}
	sword := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "wooden_sword", Quantity: 1, EquipSlot: "weapon", Stats: &itemv1.ItemStats{PhysicalAttack: 10, CritRate: 5}}
	inv, err := clients.NewItems(listing{items: []*itemv1.Item{potion, bow, sword}}).Inventory(context.Background(), kernel.NewCharacterID())
	if err != nil {
		t.Fatal(err)
	}
	wantBow := model.ItemView{ID: bow.GetId(), Template: "short_bow", Tab: "equip", Quantity: 1, Stats: &model.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, Accuracy: 11, Evasion: 12, CritRate: 13, CritDamage: 14, Penetration: 15, HPRegen: 16, CooldownSpeed: 17, MoveSpeed: 18}}
	wantSword := model.ItemView{ID: sword.GetId(), Template: "wooden_sword", Quantity: 1, Stats: &model.ItemStats{PhysicalAttack: 10, CritRate: 5}}
	want := model.Inventory{
		Revision:  9,
		Items:     []model.ItemView{{ID: potion.GetId(), Template: "red_potion", Tab: "use", Slot: 2, Quantity: 5}, wantBow},
		Equipment: []model.EquipSlot{{Slot: "weapon", Item: &wantSword}},
	}
	if !reflect.DeepEqual(inv, want) {
		t.Fatalf("inventory %+v\nwant %+v: the tabs, and the sword worn in no tab", inv, want)
	}
}
