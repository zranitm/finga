package item

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
)

// Encoders registers the encoders of the pushes the module owns.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		out.Inventory, out.Equipment, out.InventoryRevision = itemViews(w.Inventory), equipment(w.Equipment), w.InventoryRevision
	})
	return errors.Join(
		transport.Encode(e, func(p model.PiecesAdded) any {
			pieces := make([]*clientv1.AddedPiece, 0, len(p.Pieces))
			for _, a := range p.Pieces {
				pieces = append(pieces, &clientv1.AddedPiece{Id: a.ID, Cause: a.Cause})
			}
			return &clientv1.PiecesAdded{Pieces: pieces}
		}),
	)
}

// InventoryChanged is the message for slots and equip slots an inventory command, a pickup or an equip changed.
func InventoryChanged(ic model.InventoryChanged) *clientv1.InventoryChanged {
	out := make([]*clientv1.InventorySlot, 0, len(ic.Slots))
	for _, s := range ic.Slots {
		slot := &clientv1.InventorySlot{Tab: s.Tab, Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = itemView(*s.Item)
		}
		out = append(out, slot)
	}
	return &clientv1.InventoryChanged{Slots: out, Revision: ic.Revision, Equipment: equipment(ic.Equipment)}
}

func itemViews(items []model.ItemView) []*clientv1.ItemView {
	out := make([]*clientv1.ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, itemView(it))
	}
	return out
}

func itemView(it model.ItemView) *clientv1.ItemView {
	return &clientv1.ItemView{ItemId: it.ID, Template: it.Template, Tab: it.Tab, Slot: int32(it.Slot), Quantity: int32(it.Quantity), Stats: transport.ItemStats(it.Stats)}
}

func equipment(slots []model.EquipSlot) []*clientv1.EquipSlot {
	if len(slots) == 0 {
		return nil
	}
	out := make([]*clientv1.EquipSlot, 0, len(slots))
	for _, s := range slots {
		slot := &clientv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = itemView(*s.Item)
		}
		out = append(out, slot)
	}
	return out
}
