package clients_test

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/clients"
)

// listing is an item service whose Inventory answers items.
type listing struct {
	itemv1.ItemServiceClient
	items []*itemv1.Item
}

func (l listing) Inventory(context.Context, *itemv1.InventoryRequest, ...grpc.CallOption) (*itemv1.InventoryResponse, error) {
	return &itemv1.InventoryResponse{Items: l.items, Revision: 9}, nil
}

func TestAnInventoryIsWhatTheTabsHold(t *testing.T) {
	potion := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "red_potion", Tab: "use", Slot: 2, Quantity: 5}
	sword := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "wooden_sword", Quantity: 1, EquipSlot: "weapon"}
	inv, err := clients.NewItems(listing{items: []*itemv1.Item{potion, sword}}).Inventory(context.Background(), kernel.NewCharacterID())
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Items) != 1 || inv.Items[0].ID != potion.GetId() || inv.Items[0].Slot != 2 || inv.Revision != 9 {
		t.Fatalf("inventory %+v; want the potion alone, the worn sword in no tab", inv)
	}
}
