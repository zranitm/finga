package receive

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/model"
)

// hello checks the protocol version and character ID, then joins; a rejected Hello is kicked.
func (h *Handler) hello(c *msgContext, in *clientv1.Hello) (any, error) {
	inbox := c.Session
	if in.GetProtocolVersion() != protocol.Version {
		inbox.out.Kick(clientv1.KickReason_KICK_REASON_OUTDATED_PROTOCOL)
		return nil, nil
	}
	id, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		inbox.out.Kick(clientv1.KickReason_KICK_REASON_UNAUTHENTICATED)
		return nil, nil
	}
	switch err := h.gw.Hello(c.Ctx, inbox.session, in.GetToken(), id); {
	case err == nil:
		inbox.joined = true
	case errors.Is(err, model.ErrHelloRejected):
		inbox.out.Kick(clientv1.KickReason_KICK_REASON_UNAUTHENTICATED)
	default:
		inbox.out.Close(wsconn.CloseInternal, "hello")
	}
	return nil, nil
}

// ping answers at once, on the read loop.
func (h *Handler) ping(c *msgContext, p *clientv1.Ping) (any, error) {
	c.Session.out.Reply("", &clientv1.Pong{ClientTime: p.GetClientTime(), ServerTime: h.opts.Clock().UnixMilli()})
	return nil, nil
}

func (h *Handler) input(c *msgContext, in *clientv1.Input) (any, error) {
	keys := model.Keys{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Down: in.GetDown(), Jump: in.GetJump()}
	return nil, h.gw.Input(c.Ctx, c.Session.session, c.Message.Seq, keys)
}

func (h *Handler) attack(c *msgContext, _ *clientv1.Attack) (any, error) {
	return nil, h.gw.Attack(c.Ctx, c.Session.session)
}

func (h *Handler) pickUp(c *msgContext, _ *clientv1.PickUp) (any, error) {
	return nil, h.gw.PickUp(c.Ctx, c.Session.session)
}

func (h *Handler) enterPortal(c *msgContext, _ *clientv1.EnterPortal) (any, error) {
	return nil, h.gw.EnterPortal(c.Ctx, c.Session.session)
}

func (h *Handler) inventoryMove(c *msgContext, in *clientv1.InventoryMove) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return changed(h.gw.MoveItem(c.Ctx, c.Session.session, model.ItemMove{IdempotencyKey: in.GetIdempotencyKey(), Item: id, Tab: in.GetTab(), Slot: int(in.GetSlot())}))
}

func (h *Handler) inventoryDiscard(c *msgContext, in *clientv1.InventoryDiscard) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return changed(h.gw.DiscardItem(c.Ctx, c.Session.session, model.ItemDiscard{IdempotencyKey: in.GetIdempotencyKey(), Item: id, Quantity: int(in.GetQuantity())}))
}

// changed is an inventory command's follow-up: the changed slots, if any.
func changed(slots []*clientv1.InventorySlot, err error) (any, error) {
	if err != nil || len(slots) == 0 {
		return nil, err
	}
	return &clientv1.InventoryChanged{Slots: slots}, nil
}

func (h *Handler) partyInvite(c *msgContext, in *clientv1.PartyInvite) (any, error) {
	to, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.InviteToParty(c.Ctx, c.Session.session, to)
}

func (h *Handler) partyRespond(c *msgContext, in *clientv1.PartyRespond) (any, error) {
	return nil, h.gw.AnswerInvite(c.Ctx, c.Session.session, in.GetInviteId(), in.GetAccept())
}

func (h *Handler) partyLeave(c *msgContext, _ *clientv1.PartyLeave) (any, error) {
	return nil, h.gw.LeaveParty(c.Ctx, c.Session.session)
}

func (h *Handler) partyKick(c *msgContext, in *clientv1.PartyKick) (any, error) {
	target, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.KickFromParty(c.Ctx, c.Session.session, target)
}

func (h *Handler) partyPromote(c *msgContext, in *clientv1.PartyPromote) (any, error) {
	target, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.PromoteInParty(c.Ctx, c.Session.session, target)
}

func (h *Handler) chatSend(c *msgContext, in *clientv1.ChatSend) (any, error) {
	return nil, h.gw.Chat(c.Ctx, c.Session.session, in.GetChannel(), in.GetText())
}
