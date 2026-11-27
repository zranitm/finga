package item

import (
	"context"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// Items is the item module as the client's messages call it.
type Items interface {
	Move(ctx context.Context, actor item.Actor, r item.MoveRequest) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, r item.DiscardRequest) (item.Result, error)
	DiscardMany(ctx context.Context, actor item.Actor, r item.DiscardManyRequest) (item.Result, error)
	AddToWardrobe(ctx context.Context, actor item.Actor, r item.WardrobeRequest) (item.Result, error)
}

// itemCallFor is all a call to the item module for a command may take: inside the 5 s a command is given, so the command answers unavailable rather than failing at that deadline.
const itemCallFor = 3500 * time.Millisecond

// ws is the module's client messages: client to server.
type ws struct {
	items Items
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(items Items) transport.Routes {
	h := &ws{items: items}
	return func(r *transport.Receiver) {
		transport.Handle(r, "InventoryMove", h.inventoryMove, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "InventoryDiscard", h.inventoryDiscard, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "InventoryDiscardMany", h.inventoryDiscardMany, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "AddToWardrobe", h.addToWardrobe, r.Command(transport.LimitInventory, r.Respond)...)
	}
}

// within bounds a call to the item module by itemCallFor.
func within(ctx context.Context, what string, call func(ctx context.Context) (item.Result, error)) (item.Result, error) {
	return transient.Within(ctx, itemCallFor, "the item module, asked for "+what, call)
}

func actorOf(c *transport.Context) item.Actor {
	a := c.Session.Session().Actor()
	return item.Actor{Account: a.Account, Character: a.Character}
}

func (h *ws) inventoryMove(c *transport.Context, in *clientv1.InventoryMove) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	res, err := within(c.Ctx, "a move", func(ctx context.Context) (item.Result, error) {
		return h.items.Move(ctx, actorOf(c), item.MoveRequest{Key: in.GetIdempotencyKey(), ItemID: id, Tab: gamedata.Tab(in.GetTab()), Slot: int(in.GetSlot())})
	})
	return changed(res, err)
}

func (h *ws) inventoryDiscard(c *transport.Context, in *clientv1.InventoryDiscard) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	res, err := within(c.Ctx, "a discard", func(ctx context.Context) (item.Result, error) {
		return h.items.Discard(ctx, actorOf(c), item.DiscardRequest{Key: in.GetIdempotencyKey(), ItemID: id, Quantity: int(in.GetQuantity())})
	})
	return changed(res, err)
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
	res, err := within(c.Ctx, "a discard", func(ctx context.Context) (item.Result, error) {
		return h.items.DiscardMany(ctx, actorOf(c), item.DiscardManyRequest{Key: in.GetIdempotencyKey(), ItemIDs: ids})
	})
	return changed(res, err)
}

// addToWardrobe answers with the slot it took from; the piece is the item's, which the client knows. The connection hears PiecesAdded, as a grant's would; a replayed add repeats it harmlessly.
func (h *ws) addToWardrobe(c *transport.Context, in *clientv1.AddToWardrobe) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	res, err := within(c.Ctx, "a wardrobe add", func(ctx context.Context) (item.Result, error) {
		return h.items.AddToWardrobe(ctx, actorOf(c), item.WardrobeRequest{Key: in.GetIdempotencyKey(), ItemID: id})
	})
	if err != nil {
		return nil, err
	}
	c.Session.Session().Deliver(model.Delivery{Push: model.PiecesAdded{Pieces: []model.AddedPiece{{ID: res.Piece, Cause: item.PieceFromItem}}}})
	return changed(res, nil)
}

// changed is an inventory command's follow-up: the changed slots, if any.
func changed(res item.Result, err error) (any, error) {
	if err != nil || len(res.Changed) == 0 {
		return nil, err
	}
	return inventoryChanged(res), nil
}

// inventoryChanged is the message for the slots a command changed.
func inventoryChanged(res item.Result) *clientv1.InventoryChanged {
	slots := make([]*clientv1.InventorySlot, 0, len(res.Changed))
	for _, s := range res.Changed {
		slot := &clientv1.InventorySlot{Tab: string(s.Tab), Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = wireItem(*s.Item)
		}
		slots = append(slots, slot)
	}
	return &clientv1.InventoryChanged{Slots: slots, Revision: res.Revision}
}

func wireItem(it item.Item) *clientv1.ItemView {
	return &clientv1.ItemView{ItemId: it.ID.String(), Template: it.TemplateID, Tab: string(it.Tab), Slot: int32(it.Slot), Quantity: int32(it.Quantity), Stats: transport.ItemStats((*model.ItemStats)(it.Stats))}
}
