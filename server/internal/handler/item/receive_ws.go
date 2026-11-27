package item

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/gateway/send"
	"finga/internal/handler/transport"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
)

// ws is the module's client messages: client to server.
type ws struct {
	gw *send.Gateway
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(gw *send.Gateway) transport.Routes {
	h := &ws{gw: gw}
	return func(r *transport.Receiver) {
		transport.Handle(r, "InventoryMove", h.inventoryMove, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "InventoryDiscard", h.inventoryDiscard, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "InventoryDiscardMany", h.inventoryDiscardMany, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "AddToWardrobe", h.addToWardrobe, r.Command(transport.LimitInventory, r.Respond)...)
	}
}

func (h *ws) inventoryMove(c *transport.Context, in *clientv1.InventoryMove) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return changed(h.gw.MoveItem(c.Ctx, c.Session.Session(), model.ItemMove{IdempotencyKey: in.GetIdempotencyKey(), Item: id, Tab: in.GetTab(), Slot: int(in.GetSlot())}))
}

func (h *ws) inventoryDiscard(c *transport.Context, in *clientv1.InventoryDiscard) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return changed(h.gw.DiscardItem(c.Ctx, c.Session.Session(), model.ItemDiscard{IdempotencyKey: in.GetIdempotencyKey(), Item: id, Quantity: int(in.GetQuantity())}))
}

func (h *ws) inventoryDiscardMany(c *transport.Context, in *clientv1.InventoryDiscardMany) (any, error) {
	ids := make([]kernel.ItemID, 0, len(in.GetItemIds()))
	for _, raw := range in.GetItemIds() {
		id, err := kernel.ParseItemID(raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return changed(h.gw.DiscardItems(c.Ctx, c.Session.Session(), model.ItemDiscardMany{IdempotencyKey: in.GetIdempotencyKey(), Items: ids}))
}

// addToWardrobe answers with the slot it took from; the piece is the item's, which the client knows.
func (h *ws) addToWardrobe(c *transport.Context, in *clientv1.AddToWardrobe) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return changed(h.gw.AddToWardrobe(c.Ctx, c.Session.Session(), model.WardrobeAdd{IdempotencyKey: in.GetIdempotencyKey(), Item: id}))
}

// changed is an inventory command's follow-up: the changed slots, if any.
func changed(ic model.InventoryChanged, err error) (any, error) {
	if err != nil || len(ic.Slots) == 0 {
		return nil, err
	}
	return InventoryChanged(ic), nil
}
