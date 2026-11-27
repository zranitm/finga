package item

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
)

// piecesAdded is the wardrobe piece an item gave the account, which the add-to-wardrobe command tells the connection of, as a grant's would be.
type piecesAdded struct{ Piece string }

// Encoders registers the encoders of the pushes the module owns, and its part of the Welcome: the inventory and what is worn.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		inv, ok := w.Inventory.(item.Inventory)
		if !ok {
			return
		}
		out.Inventory, out.Equipment, out.InventoryRevision = inventoryItems(inv), worn(inv), inv.Revision
	})
	return errors.Join(
		transport.Encode(e, func(p piecesAdded) any { return pieceAdded(p) }),
	)
}

// inventoryItems are the items in the character's tabs, an empty list when it has none.
func inventoryItems(inv item.Inventory) []*clientv1.ItemView {
	out := make([]*clientv1.ItemView, 0, len(inv.Items))
	for _, it := range inv.Items {
		if it.EquipSlot == "" {
			out = append(out, itemView(it, string(it.Tab), it.Slot))
		}
	}
	return out
}

// worn are the equip slots with what each holds; a worn item is in no tab.
func worn(inv item.Inventory) []*clientv1.EquipSlot {
	var out []*clientv1.EquipSlot
	for _, it := range inv.Items {
		if it.EquipSlot != "" {
			out = append(out, &clientv1.EquipSlot{EquipSlot: it.EquipSlot, Item: itemView(it, "", 0)})
		}
	}
	return out
}

// inventoryChanged is the message for the slots a command changed.
func inventoryChanged(res item.Result) *clientv1.InventoryChanged {
	slots := make([]*clientv1.InventorySlot, 0, len(res.Changed))
	for _, s := range res.Changed {
		slot := &clientv1.InventorySlot{Tab: string(s.Tab), Slot: int32(s.Slot)} //nolint:gosec // a slot is small
		if s.Item != nil {
			slot.Item = itemView(*s.Item, string(s.Item.Tab), s.Item.Slot)
		}
		slots = append(slots, slot)
	}
	return &clientv1.InventoryChanged{Slots: slots, Revision: res.Revision}
}

// itemView is an item on the wire, in the tab and slot named.
func itemView(it item.Item, tab string, slot int) *clientv1.ItemView {
	return &clientv1.ItemView{ItemId: it.ID.String(), Template: it.TemplateID, Tab: tab, Slot: int32(slot), Quantity: int32(it.Quantity), Stats: transport.ItemStats(it.Stats)} //nolint:gosec // a slot and a stack are small
}

// pieceAdded tells of the wardrobe piece an item gave, as the item's.
func pieceAdded(p piecesAdded) *clientv1.PiecesAdded {
	return &clientv1.PiecesAdded{Pieces: []*clientv1.AddedPiece{{Id: p.Piece, Cause: item.PieceFromItem}}}
}
