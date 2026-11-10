package receive

import (
	"errors"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/handler/ws/push"
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
	c.Session.out.Reply("", &clientv1.Pong{ClientTime: p.GetClientTime(), ServerTime: time.Now().UnixMilli()})
	return nil, nil
}

func (h *Handler) input(c *msgContext, in *clientv1.Input) (any, error) {
	keys := model.Keys{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Down: in.GetDown(), Jump: in.GetJump()}
	return nil, h.gw.Input(c.Ctx, c.Session.session, c.Message.Seq, keys)
}

func (h *Handler) attack(c *msgContext, _ *clientv1.Attack) (any, error) {
	return nil, h.gw.Attack(c.Ctx, c.Session.session)
}

// ignore answers a retired PickUp an older client still sends with nothing: drops fly to their owners by themselves.
func (h *Handler) ignore(*msgContext, *clientv1.PickUp) (any, error) { return nil, nil }

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

// equip answers once the world applied it; the changed slots and stats follow on the world's stream.
func (h *Handler) equip(c *msgContext, in *clientv1.Equip) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.Equip(c.Ctx, c.Session.session, model.ItemEquip{IdempotencyKey: in.GetIdempotencyKey(), Item: id, EquipSlot: in.GetEquipSlot()})
}

func (h *Handler) unequip(c *msgContext, in *clientv1.Unequip) (any, error) {
	out := model.ItemUnequip{IdempotencyKey: in.GetIdempotencyKey(), EquipSlot: in.GetEquipSlot()}
	if in.ToSlot != nil {
		to := int(in.GetToSlot())
		out.ToSlot = &to
	}
	return nil, h.gw.Unequip(c.Ctx, c.Session.session, out)
}

// spendPoints answers once the world applied the points; StatsChanged comes first, on the world's stream.
func (h *Handler) spendPoints(c *msgContext, in *clientv1.SpendPoints) (any, error) {
	p := in.GetPoints()
	pts := model.Stats{STR: int(p.GetStr()), DEX: int(p.GetDex()), INT: int(p.GetInt()), AGI: int(p.GetAgi()), VIT: int(p.GetVit())}
	return nil, h.gw.SpendPoints(c.Ctx, c.Session.session, model.PointsSpend{ExpectedUnspent: int(in.GetExpectedUnspent()), Points: pts})
}

func (h *Handler) resetPoints(c *msgContext, in *clientv1.ResetPoints) (any, error) {
	return nil, h.gw.ResetPoints(c.Ctx, c.Session.session, int(in.GetExpectedUnspent()))
}

// changed is an inventory command's follow-up: the changed slots, if any.
func changed(ic model.InventoryChanged, err error) (any, error) {
	if err != nil || len(ic.Slots) == 0 {
		return nil, err
	}
	return push.InventoryChanged(ic), nil
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
	return nil, h.gw.Chat(c.Ctx, c.Session.session, chatChannels[in.GetChannel()], in.GetText())
}

// chatChannels maps the client's channels to the gateway's; any other is no channel, which the social service refuses.
var chatChannels = map[clientv1.ChatChannel]model.ChatChannel{
	clientv1.ChatChannel_CHAT_CHANNEL_MAP:   model.ChatMap,
	clientv1.ChatChannel_CHAT_CHANNEL_PARTY: model.ChatParty,
}
