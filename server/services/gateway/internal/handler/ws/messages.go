package ws

import (
	"errors"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/services/gateway/internal/model"
)

// hello checks the protocol version and character ID, then joins; a rejected Hello is kicked.
func (h *Handler) hello(c *msgContext, in protocol.Hello) (any, error) {
	cl := c.Session
	if in.ProtocolVersion != protocol.Version {
		cl.Kick(protocol.KickOutdatedProtocol)
		return nil, nil
	}
	id, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		cl.Kick(protocol.KickUnauthenticated)
		return nil, nil
	}
	switch err := h.gw.Hello(c.Ctx, cl.session, in.Token, id); {
	case err == nil:
		cl.joined = true
	case errors.Is(err, model.ErrHelloRejected):
		cl.Kick(protocol.KickUnauthenticated)
	default:
		cl.Close("hello")
	}
	return nil, nil
}

// ping answers at once, on the read loop.
func (h *Handler) ping(c *msgContext, p protocol.Ping) (any, error) {
	c.Session.reply("", protocol.Pong{ClientTime: p.ClientTime, ServerTime: h.opts.Clock().UnixMilli()})
	return nil, nil
}

func (h *Handler) input(c *msgContext, in protocol.Input) (any, error) {
	keys := model.Keys{Left: in.Left, Right: in.Right, Up: in.Up, Down: in.Down, Jump: in.Jump}
	return nil, h.gw.Input(c.Ctx, c.Session.session, c.Message.Seq, keys)
}

func (h *Handler) attack(c *msgContext, _ protocol.Attack) (any, error) {
	return nil, h.gw.Attack(c.Ctx, c.Session.session)
}

func (h *Handler) pickUp(c *msgContext, _ protocol.PickUp) (any, error) {
	return nil, h.gw.PickUp(c.Ctx, c.Session.session)
}

func (h *Handler) enterPortal(c *msgContext, _ protocol.EnterPortal) (any, error) {
	return nil, h.gw.EnterPortal(c.Ctx, c.Session.session)
}

func (h *Handler) inventoryMove(c *msgContext, in protocol.InventoryMove) (any, error) {
	id, err := kernel.ParseItemID(in.ItemID)
	if err != nil {
		return nil, err
	}
	return changed(h.gw.MoveItem(c.Ctx, c.Session.session, model.ItemMove{IdempotencyKey: in.IdempotencyKey, Item: id, Tab: in.Tab, Slot: in.Slot}))
}

func (h *Handler) inventoryDiscard(c *msgContext, in protocol.InventoryDiscard) (any, error) {
	id, err := kernel.ParseItemID(in.ItemID)
	if err != nil {
		return nil, err
	}
	return changed(h.gw.DiscardItem(c.Ctx, c.Session.session, model.ItemDiscard{IdempotencyKey: in.IdempotencyKey, Item: id, Quantity: in.Quantity}))
}

// changed is an inventory command's follow-up: the changed slots, if any.
func changed(slots []protocol.InventorySlot, err error) (any, error) {
	if err != nil || len(slots) == 0 {
		return nil, err
	}
	return protocol.InventoryChanged{Slots: slots}, nil
}

func (h *Handler) partyInvite(c *msgContext, in protocol.PartyInvite) (any, error) {
	to, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		return nil, err
	}
	return nil, h.gw.InviteToParty(c.Ctx, c.Session.session, to)
}

func (h *Handler) partyRespond(c *msgContext, in protocol.PartyRespond) (any, error) {
	return nil, h.gw.AnswerInvite(c.Ctx, c.Session.session, in.InviteID, in.Accept)
}

func (h *Handler) partyLeave(c *msgContext, _ protocol.PartyLeave) (any, error) {
	return nil, h.gw.LeaveParty(c.Ctx, c.Session.session)
}

func (h *Handler) partyKick(c *msgContext, in protocol.PartyKick) (any, error) {
	target, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		return nil, err
	}
	return nil, h.gw.KickFromParty(c.Ctx, c.Session.session, target)
}

func (h *Handler) partyPromote(c *msgContext, in protocol.PartyPromote) (any, error) {
	target, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		return nil, err
	}
	return nil, h.gw.PromoteInParty(c.Ctx, c.Session.session, target)
}

func (h *Handler) chatSend(c *msgContext, in protocol.ChatSend) (any, error) {
	return nil, h.gw.Chat(c.Ctx, c.Session.session, in.Channel, in.Text)
}
