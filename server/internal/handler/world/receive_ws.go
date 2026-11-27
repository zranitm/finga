package world

import (
	application "finga/internal/application/account"
	"finga/internal/shared/failure"
	"math"
	"strconv"
	"strings"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// ws is the module's client messages: client to server.
type ws struct {
	gw *application.Commands
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(gw *application.Commands) transport.Routes {
	h := &ws{gw: gw}
	return func(r *transport.Receiver) {
		transport.Handle(r, "Input", h.input, r.Action(transport.LimitInput, r.Logged)...)
		transport.Handle(r, "Attack", h.attack, r.Action(transport.LimitAttack, r.Logged)...)
		transport.Handle(r, "TakeLoot", h.takeLoot, r.Command(transport.LimitLoot, r.Respond)...)
		transport.Handle(r, "EnterPortal", h.enterPortal, r.Action(transport.LimitPortal, r.Logged)...)
		transport.Handle(r, "Teleport", h.teleport, r.Action(transport.LimitTeleport, r.Respond)...)
		transport.Handle(r, "SwitchChannel", h.switchChannel, r.Action(transport.LimitTeleport, r.Respond)...)
		transport.Handle(r, "Channels", h.channels, r.Command(transport.LimitChannels, r.Respond)...)
		transport.Handle(r, "Bosses", h.bosses, r.Command(transport.LimitBosses, r.Respond)...)
		transport.Handle(r, "Equip", h.equip, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "Unequip", h.unequip, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "UseItem", h.useItem, r.Command(transport.LimitPotion, r.Respond)...)
		transport.Handle(r, "SetLook", h.setLook, r.Command(transport.LimitInventory, r.Respond)...)
		transport.Handle(r, "SpendPoints", h.spendPoints, r.Command(transport.LimitPoints, r.Respond)...)
		transport.Handle(r, "ResetPoints", h.resetPoints, r.Command(transport.LimitPoints, r.Respond)...)
		transport.Handle(r, "SpendSkillPoints", h.spendSkillPoints, r.Command(transport.LimitPoints, r.Respond)...)
		transport.Handle(r, "ChooseTrait", h.chooseTrait, r.Command(transport.LimitPoints, r.Respond)...)
		transport.Handle(r, "UseSkill", h.useSkill, r.Action(transport.LimitSkill, r.Respond)...)
		transport.Handle(r, "Talk", h.talk, r.Command(transport.LimitDialog, r.Respond)...)
		transport.Handle(r, "Promote", h.promote, r.Command(transport.LimitDialog, r.Respond)...)
		transport.Handle(r, "AcceptQuest", h.acceptQuest, r.Command(transport.LimitDialog, r.Respond)...)
		transport.Handle(r, "AbandonQuest", h.abandonQuest, r.Command(transport.LimitDialog, r.Respond)...)
		transport.Handle(r, "HandInQuest", h.handInQuest, r.Command(transport.LimitDialog, r.Respond)...)
	}
}

func (h *ws) input(c *transport.Context, in *clientv1.Input) (any, error) {
	keys := model.Keys{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Down: in.GetDown(), Jump: in.GetJump(), Walk: in.GetWalk()}
	return nil, h.gw.Input(c.Ctx, c.Session.Session(), c.Message.Seq, keys, in.GetView())
}

func (h *ws) attack(c *transport.Context, in *clientv1.Attack) (any, error) {
	aim, err := aimOf(in.GetAim())
	if err != nil {
		return nil, err
	}
	target, err := targetOf(in.GetTarget())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.Attack(c.Ctx, c.Session.Session(), aim, target)
}

// takeLoot answers each pick once the item module did: taken with code 0, or refused with the code of why.
func (h *ws) takeLoot(c *transport.Context, in *clientv1.TakeLoot) (any, error) {
	if len(in.GetPicks()) > failure.MaxPicks {
		return nil, failure.ErrTooManyPicks
	}
	picks := make([]model.LootPick, 0, len(in.GetPicks()))
	for _, p := range in.GetPicks() {
		picks = append(picks, model.LootPick{Pouch: uint64(p.GetPouch()), Item: p.GetItem()})
	}
	answers, err := h.gw.TakeLoot(c.Ctx, c.Session.Session(), picks)
	if err != nil {
		return nil, err
	}
	out := &clientv1.Response{Ok: true, Picks: make([]*clientv1.PickAnswer, 0, len(answers))}
	for _, a := range answers {
		answer := &clientv1.PickAnswer{Pick: &clientv1.LootPick{Pouch: uint32(a.Pouch), Item: a.Item}} //nolint:gosec // pouch IDs count up within one map instance
		if a.Err != nil {
			answer.Code = int32(failure.PublicOf(a.Err).Code) //nolint:gosec // six-digit codes
		}
		out.Picks = append(out.Picks, answer)
	}
	return out, nil
}

func (h *ws) enterPortal(c *transport.Context, _ *clientv1.EnterPortal) (any, error) {
	return nil, h.gw.EnterPortal(c.Ctx, c.Session.Session())
}

// teleport answers once the world started the channel; its MapChange or TeleportCancelled comes on the world's stream.
func (h *ws) teleport(c *transport.Context, in *clientv1.Teleport) (any, error) {
	return nil, h.gw.Teleport(c.Ctx, c.Session.Session(), in.GetMapId(), in.GetWaypoint(), c.Message.Seq)
}

// switchChannel answers once the world started the switch; its MapChange or TeleportCancelled comes on the world's stream.
func (h *ws) switchChannel(c *transport.Context, in *clientv1.SwitchChannel) (any, error) {
	return nil, h.gw.SwitchChannel(c.Ctx, c.Session.Session(), int(in.GetChannel()), c.Message.Seq)
}

// channels answers with the region's open channels.
func (h *ws) channels(c *transport.Context, _ *clientv1.Channels) (any, error) {
	list, err := h.gw.Channels(c.Ctx, c.Session.Session())
	if err != nil {
		return nil, err
	}
	return &clientv1.Response{Ok: true, Channels: ChannelList(list)}, nil
}

// bosses answers with how each of the region's area bosses stands.
func (h *ws) bosses(c *transport.Context, _ *clientv1.Bosses) (any, error) {
	list, err := h.gw.Bosses(c.Ctx, c.Session.Session())
	if err != nil {
		return nil, err
	}
	return &clientv1.Response{Ok: true, Bosses: BossList(list)}, nil
}

// equip answers once the world applied it; the changed slots and stats come on the world's stream, before or after the answer.
func (h *ws) equip(c *transport.Context, in *clientv1.Equip) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.Equip(c.Ctx, c.Session.Session(), model.ItemEquip{IdempotencyKey: in.GetIdempotencyKey(), Item: id, EquipSlot: in.GetEquipSlot()})
}

func (h *ws) unequip(c *transport.Context, in *clientv1.Unequip) (any, error) {
	out := model.ItemUnequip{IdempotencyKey: in.GetIdempotencyKey(), EquipSlot: in.GetEquipSlot()}
	if in.ToSlot != nil {
		to := int(in.GetToSlot())
		out.ToSlot = &to
	}
	return nil, h.gw.Unequip(c.Ctx, c.Session.Session(), out)
}

// useItem answers once the world applied the potion; the changed slot and stats come on the world's stream, before or after the answer.
func (h *ws) useItem(c *transport.Context, in *clientv1.UseItem) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.UseItem(c.Ctx, c.Session.Session(), model.ItemUse{IdempotencyKey: in.GetIdempotencyKey(), Item: id, Template: in.GetTemplate()})
}

// setLook answers once the world applied it; the look comes in the player's next view.
func (h *ws) setLook(c *transport.Context, in *clientv1.SetLook) (any, error) {
	l := in.GetLook()
	look := model.Look{Outfit: l.GetOutfit(), Headwear: l.GetHeadwear(), Face: l.GetFace(), Weapon: l.GetWeapon(), Wings: l.GetWings(), HideHeadwear: l.GetHideHeadwear()}
	return nil, h.gw.SetLook(c.Ctx, c.Session.Session(), model.LookSet{IdempotencyKey: in.GetIdempotencyKey(), Look: look})
}

// spendPoints answers once the world applied the points; StatsChanged comes on the world's stream, before or after the answer.
func (h *ws) spendPoints(c *transport.Context, in *clientv1.SpendPoints) (any, error) {
	p := in.GetPoints()
	pts := model.Stats{STR: int(p.GetStr()), DEX: int(p.GetDex()), INT: int(p.GetInt()), AGI: int(p.GetAgi()), VIT: int(p.GetVit())}
	return nil, h.gw.SpendPoints(c.Ctx, c.Session.Session(), model.PointsSpend{ExpectedUnspent: int(in.GetExpectedUnspent()), Points: pts})
}

func (h *ws) resetPoints(c *transport.Context, in *clientv1.ResetPoints) (any, error) {
	return nil, h.gw.ResetPoints(c.Ctx, c.Session.Session(), int(in.GetExpectedUnspent()))
}

// spendSkillPoints answers once the world applied the skill points; StatsChanged comes on the world's stream, before or after the answer.
func (h *ws) spendSkillPoints(c *transport.Context, in *clientv1.SpendSkillPoints) (any, error) {
	pts := make(map[string]int, len(in.GetPoints()))
	for skill, n := range in.GetPoints() {
		pts[skill] = int(n)
	}
	return nil, h.gw.SpendSkillPoints(c.Ctx, c.Session.Session(), model.SkillPointsSpend{ExpectedUnspent: int(in.GetExpectedUnspent()), Points: pts})
}

// chooseTrait answers once the world set the trait; StatsChanged comes on the world's stream, before or after the answer.
func (h *ws) chooseTrait(c *transport.Context, in *clientv1.ChooseTrait) (any, error) {
	return nil, h.gw.ChooseTrait(c.Ctx, c.Session.Session(), model.TraitChoice{Skill: in.GetSkill(), Trait: in.GetTrait()})
}

// useSkill answers once the world's tick resolved the skill; what it did comes on the world's stream, before or after the answer.
func (h *ws) useSkill(c *transport.Context, in *clientv1.UseSkill) (any, error) {
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
	return nil, h.gw.UseSkill(c.Ctx, c.Session.Session(), use, c.Message.Seq)
}

func (h *ws) talk(c *transport.Context, in *clientv1.Talk) (any, error) {
	return nil, h.gw.Talk(c.Ctx, c.Session.Session(), in.GetNpcId())
}

// promote answers once the promotion applied; Promoted comes on the world's stream.
func (h *ws) promote(c *transport.Context, in *clientv1.Promote) (any, error) {
	return nil, h.gw.Promote(c.Ctx, c.Session.Session(), in.GetNpcId(), in.GetClass())
}

func (h *ws) acceptQuest(c *transport.Context, in *clientv1.AcceptQuest) (any, error) {
	return nil, h.gw.AcceptQuest(c.Ctx, c.Session.Session(), model.QuestAsk{NPC: in.GetNpcId(), Quest: in.GetQuestId()})
}

func (h *ws) abandonQuest(c *transport.Context, in *clientv1.AbandonQuest) (any, error) {
	return nil, h.gw.AbandonQuest(c.Ctx, c.Session.Session(), in.GetQuestId())
}

func (h *ws) handInQuest(c *transport.Context, in *clientv1.HandInQuest) (any, error) {
	return nil, h.gw.HandInQuest(c.Ctx, c.Session.Session(), model.QuestAsk{NPC: in.GetNpcId(), Quest: in.GetQuestId()})
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
		return 0, failure.ErrBadAim
	}
	return yaw, nil
}
