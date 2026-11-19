package receive

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/gateway/ws/push"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
)

// hello checks the protocol version and character ID, then joins; a rejected Hello is kicked.
func (h *Handler) hello(c *msgContext, in *clientv1.Hello) (any, error) {
	inbox := c.Session
	defer close(inbox.hello) // the actions wait for it
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
	keys := model.Keys{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Down: in.GetDown(), Jump: in.GetJump(), Walk: in.GetWalk()}
	return nil, h.gw.Input(c.Ctx, c.Session.session, c.Message.Seq, keys, in.GetView())
}

func (h *Handler) attack(c *msgContext, in *clientv1.Attack) (any, error) {
	aim, err := aimOf(in.GetAim())
	if err != nil {
		return nil, err
	}
	target, err := targetOf(in.GetTarget())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.Attack(c.Ctx, c.Session.session, aim, target)
}

// targetOf is a lock as the client names it: "m:<mob id>" or "p:<character id>", empty for none.
func targetOf(ref string) (model.Target, error) {
	if ref == "" {
		return model.Target{}, nil
	}
	if id, ok := strings.CutPrefix(ref, "m:"); ok {
		mob, err := strconv.ParseUint(id, 10, 64)
		if err != nil || mob == 0 {
			return model.Target{}, kernel.ErrBadID
		}
		return model.Target{Mob: mob}, nil
	}
	if id, ok := strings.CutPrefix(ref, "p:"); ok {
		character, err := kernel.ParseCharacterID(id)
		if err != nil {
			return model.Target{}, err
		}
		return model.Target{Character: character}, nil
	}
	return model.Target{}, kernel.ErrBadID
}

// aimOf is a yaw an attack or a skill aims along, refused unless finite.
func aimOf(yaw float32) (float32, error) {
	if f := float64(yaw); math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, model.ErrBadAim
	}
	return yaw, nil
}

// takeLoot answers each pick once the item module did: taken with code 0, or refused with the code of why.
func (h *Handler) takeLoot(c *msgContext, in *clientv1.TakeLoot) (any, error) {
	if len(in.GetPicks()) > model.MaxPicks {
		return nil, model.ErrTooManyPicks
	}
	picks := make([]model.LootPick, 0, len(in.GetPicks()))
	for _, p := range in.GetPicks() {
		picks = append(picks, model.LootPick{Pouch: uint64(p.GetPouch()), Item: p.GetItem()})
	}
	answers, err := h.gw.TakeLoot(c.Ctx, c.Session.session, picks)
	if err != nil {
		return nil, err
	}
	out := &clientv1.Response{Ok: true, Picks: make([]*clientv1.PickAnswer, 0, len(answers))}
	for _, a := range answers {
		answer := &clientv1.PickAnswer{Pick: &clientv1.LootPick{Pouch: uint32(a.Pouch), Item: a.Item}} //nolint:gosec // pouch IDs count up within one map instance
		if a.Err != nil {
			answer.Code = int32(model.PublicOf(a.Err).Code) //nolint:gosec // six-digit codes
		}
		out.Picks = append(out.Picks, answer)
	}
	return out, nil
}

func (h *Handler) enterPortal(c *msgContext, _ *clientv1.EnterPortal) (any, error) {
	return nil, h.gw.EnterPortal(c.Ctx, c.Session.session)
}

// teleport answers once the world started the channel; its MapChange or TeleportCancelled comes on the world's stream.
func (h *Handler) teleport(c *msgContext, in *clientv1.Teleport) (any, error) {
	return nil, h.gw.Teleport(c.Ctx, c.Session.session, in.GetMapId(), c.Message.Seq)
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

func (h *Handler) inventoryDiscardMany(c *msgContext, in *clientv1.InventoryDiscardMany) (any, error) {
	ids := make([]kernel.ItemID, 0, len(in.GetItemIds()))
	for _, raw := range in.GetItemIds() {
		id, err := kernel.ParseItemID(raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return changed(h.gw.DiscardItems(c.Ctx, c.Session.session, model.ItemDiscardMany{IdempotencyKey: in.GetIdempotencyKey(), Items: ids}))
}

// equip answers once the world applied it; the changed slots and stats come on the world's stream, before or after the answer.
func (h *Handler) equip(c *msgContext, in *clientv1.Equip) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.Equip(c.Ctx, c.Session.session, model.ItemEquip{IdempotencyKey: in.GetIdempotencyKey(), Item: id, EquipSlot: in.GetEquipSlot()})
}

// addToWardrobe answers with the slot it took from; the piece is the item's, which the client knows.
func (h *Handler) addToWardrobe(c *msgContext, in *clientv1.AddToWardrobe) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return changed(h.gw.AddToWardrobe(c.Ctx, c.Session.session, model.WardrobeAdd{IdempotencyKey: in.GetIdempotencyKey(), Item: id}))
}

// setLook answers once the world applied it; the look comes in the player's next view.
func (h *Handler) setLook(c *msgContext, in *clientv1.SetLook) (any, error) {
	l := in.GetLook()
	look := model.Look{Outfit: l.GetOutfit(), Headwear: l.GetHeadwear(), Face: l.GetFace(), Weapon: l.GetWeapon(), Wings: l.GetWings(), HideHeadwear: l.GetHideHeadwear()}
	return nil, h.gw.SetLook(c.Ctx, c.Session.session, model.LookSet{IdempotencyKey: in.GetIdempotencyKey(), Look: look})
}

func (h *Handler) unequip(c *msgContext, in *clientv1.Unequip) (any, error) {
	out := model.ItemUnequip{IdempotencyKey: in.GetIdempotencyKey(), EquipSlot: in.GetEquipSlot()}
	if in.ToSlot != nil {
		to := int(in.GetToSlot())
		out.ToSlot = &to
	}
	return nil, h.gw.Unequip(c.Ctx, c.Session.session, out)
}

// useItem answers once the world applied the potion; the changed slot and stats come on the world's stream, before or after the answer.
func (h *Handler) useItem(c *msgContext, in *clientv1.UseItem) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.UseItem(c.Ctx, c.Session.session, model.ItemUse{IdempotencyKey: in.GetIdempotencyKey(), Item: id, Template: in.GetTemplate()})
}

// spendPoints answers once the world applied the points; StatsChanged comes on the world's stream, before or after the answer.
func (h *Handler) spendPoints(c *msgContext, in *clientv1.SpendPoints) (any, error) {
	p := in.GetPoints()
	pts := model.Stats{STR: int(p.GetStr()), DEX: int(p.GetDex()), INT: int(p.GetInt()), AGI: int(p.GetAgi()), VIT: int(p.GetVit())}
	return nil, h.gw.SpendPoints(c.Ctx, c.Session.session, model.PointsSpend{ExpectedUnspent: int(in.GetExpectedUnspent()), Points: pts})
}

// spendSkillPoints answers once the world applied the skill points; StatsChanged comes on the world's stream, before or after the answer.
func (h *Handler) spendSkillPoints(c *msgContext, in *clientv1.SpendSkillPoints) (any, error) {
	pts := make(map[string]int, len(in.GetPoints()))
	for skill, n := range in.GetPoints() {
		pts[skill] = int(n)
	}
	return nil, h.gw.SpendSkillPoints(c.Ctx, c.Session.session, model.SkillPointsSpend{ExpectedUnspent: int(in.GetExpectedUnspent()), Points: pts})
}

// chooseTrait answers once the world set the trait; StatsChanged comes on the world's stream, before or after the answer.
func (h *Handler) chooseTrait(c *msgContext, in *clientv1.ChooseTrait) (any, error) {
	return nil, h.gw.ChooseTrait(c.Ctx, c.Session.session, model.TraitChoice{Skill: in.GetSkill(), Trait: in.GetTrait()})
}

// useSkill answers once the world's tick resolved the skill; what it did comes on the world's stream, before or after the answer.
func (h *Handler) useSkill(c *msgContext, in *clientv1.UseSkill) (any, error) {
	aim, err := aimOf(in.GetAim())
	if err != nil {
		return nil, err
	}
	target, err := targetOf(in.GetTarget())
	if err != nil {
		return nil, err
	}
	w := in.GetWay()
	use := model.SkillUse{Skill: in.GetSkill(), ChainRoot: in.GetChainRoot(), Aim: aim, Target: target, Way: model.Way{X: w.GetX(), Z: w.GetZ(), Length: w.GetLength()}}
	return nil, h.gw.UseSkill(c.Ctx, c.Session.session, use, c.Message.Seq)
}

func (h *Handler) resetPoints(c *msgContext, in *clientv1.ResetPoints) (any, error) {
	return nil, h.gw.ResetPoints(c.Ctx, c.Session.session, int(in.GetExpectedUnspent()))
}

func (h *Handler) talk(c *msgContext, in *clientv1.Talk) (any, error) {
	return nil, h.gw.Talk(c.Ctx, c.Session.session, in.GetNpcId())
}

// promote answers once the promotion applied; Promoted comes on the world's stream.
func (h *Handler) promote(c *msgContext, in *clientv1.Promote) (any, error) {
	return nil, h.gw.Promote(c.Ctx, c.Session.session, in.GetNpcId(), in.GetClass())
}

func (h *Handler) acceptQuest(c *msgContext, in *clientv1.AcceptQuest) (any, error) {
	return nil, h.gw.AcceptQuest(c.Ctx, c.Session.session, model.QuestAsk{NPC: in.GetNpcId(), Quest: in.GetQuestId()})
}

func (h *Handler) abandonQuest(c *msgContext, in *clientv1.AbandonQuest) (any, error) {
	return nil, h.gw.AbandonQuest(c.Ctx, c.Session.session, in.GetQuestId())
}

func (h *Handler) handInQuest(c *msgContext, in *clientv1.HandInQuest) (any, error) {
	return nil, h.gw.HandInQuest(c.Ctx, c.Session.session, model.QuestAsk{NPC: in.GetNpcId(), Quest: in.GetQuestId()})
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

// chatChannels maps the client's channels to the gateway's; any other is no channel, which the social module refuses.
var chatChannels = map[clientv1.ChatChannel]model.ChatChannel{
	clientv1.ChatChannel_CHAT_CHANNEL_MAP:   model.ChatMap,
	clientv1.ChatChannel_CHAT_CHANNEL_PARTY: model.ChatParty,
}

func (h *Handler) pvpQueue(c *msgContext, in *clientv1.PvpQueue) (any, error) {
	return nil, h.gw.QueuePvp(c.Ctx, c.Session.session, in.GetMode())
}

func (h *Handler) pvpLeaveQueue(c *msgContext, _ *clientv1.PvpLeaveQueue) (any, error) {
	return nil, h.gw.LeavePvpQueue(c.Ctx, c.Session.session)
}

func (h *Handler) pvpAnswer(c *msgContext, in *clientv1.PvpAnswer) (any, error) {
	return nil, h.gw.AnswerPvp(c.Ctx, c.Session.session, in.GetOffer(), in.GetAccept())
}

func (h *Handler) pvpForfeit(c *msgContext, _ *clientv1.PvpForfeit) (any, error) {
	return nil, h.gw.ForfeitMatch(c.Ctx, c.Session.session)
}
