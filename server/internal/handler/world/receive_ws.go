package world

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	"finga/internal/shared/failure"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// World is the world module as the client's messages call it.
type World interface {
	Input(ctx context.Context, p world.Presence, seq uint32, in sim.Input, view uint32) error
	Attack(ctx context.Context, p world.Presence, aim float32, target world.EntityRef) error
	EnterPortal(ctx context.Context, p world.Presence) error
	Teleport(ctx context.Context, p world.Presence, mapID, waypoint string, seq uint32) error
	SwitchChannel(ctx context.Context, p world.Presence, n int, seq uint32) error
	Channels(ctx context.Context, p world.Presence) (world.Channels, error)
	Bosses(ctx context.Context, p world.Presence) ([]world.AreaBoss, error)
	Equip(ctx context.Context, p world.Presence, key string, item kernel.ItemID, equipSlot string) error
	SetLook(ctx context.Context, p world.Presence, key string, look world.Look) error
	TakeLoot(ctx context.Context, p world.Presence, picks []world.Pick) ([]world.PickAnswer, error)
	UseItem(ctx context.Context, p world.Presence, key string, item kernel.ItemID, template string) error
	Unequip(ctx context.Context, p world.Presence, key, equipSlot string, toSlot *int) error
	SpendPoints(ctx context.Context, p world.Presence, expected int, points gamedata.Stats) error
	ResetPoints(ctx context.Context, p world.Presence, expected int) error
	SpendSkillPoints(ctx context.Context, p world.Presence, expected int, points map[string]int) error
	ChooseTrait(ctx context.Context, p world.Presence, skill, trait string) error
	UseSkill(ctx context.Context, p world.Presence, use world.SkillUse, seq uint32) error
	Promote(ctx context.Context, p world.Presence, npc string, class kernel.Class) error
	Talk(ctx context.Context, p world.Presence, npc string) error
	AcceptQuest(ctx context.Context, p world.Presence, npc, quest string) error
	AbandonQuest(ctx context.Context, p world.Presence, quest string) error
	HandInQuest(ctx context.Context, p world.Presence, npc, quest string) error
	Forfeit(ctx context.Context, p world.Presence) error
}

// ws is the module's client messages: client to server.
type ws struct {
	world World
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(w World) transport.Routes {
	h := &ws{world: w}
	return func(r *transport.Receiver) {
		transport.Handle(r, "Input", h.input, r.Action(transport.LimitInput, r.Logged)...)
		transport.Handle(r, "Attack", h.attack, r.Action(transport.LimitAttack, r.Logged)...)
		transport.Handle(r, "TakeLoot", h.takeLoot, r.Command(transport.LimitLoot, r.Respond)...)
		transport.Handle(r, "EnterPortal", h.enterPortal, r.Action(transport.LimitPortal, r.Respond)...)
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
		transport.Handle(r, "PvpForfeit", h.pvpForfeit, r.Command(transport.LimitPvp, r.Respond)...)
	}
}

func (h *ws) input(c *transport.Context, in *clientv1.Input) (any, error) {
	keys := sim.Input{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Down: in.GetDown(), Jump: in.GetJump(), Walk: in.GetWalk()}
	return nil, worldErr(h.world.Input(c.Ctx, presence(c), c.Message.Seq, keys, in.GetView()))
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
	return nil, worldErr(h.world.Attack(c.Ctx, presence(c), aim, target))
}

// takeLoot answers each pick once the item module did: taken with code 0, or refused with the code of why.
func (h *ws) takeLoot(c *transport.Context, in *clientv1.TakeLoot) (any, error) {
	if len(in.GetPicks()) > failure.MaxPicks {
		return nil, failure.ErrTooManyPicks
	}
	picks := make([]world.Pick, 0, len(in.GetPicks()))
	for _, p := range in.GetPicks() {
		picks = append(picks, world.Pick{Pouch: uint64(p.GetPouch()), Item: p.GetItem()})
	}
	answers, err := h.world.TakeLoot(c.Ctx, presence(c), picks)
	if err != nil {
		return nil, worldErr(err)
	}
	out := &clientv1.Response{Ok: true, Picks: make([]*clientv1.PickAnswer, 0, len(answers))}
	for _, a := range answers {
		answer := &clientv1.PickAnswer{Pick: &clientv1.LootPick{Pouch: uint32(a.Pouch), Item: a.Item}} //nolint:gosec // pouch IDs count up within one map instance
		if a.Err != nil {
			answer.Code = int32(failure.PublicOf(worldErr(a.Err)).Code) //nolint:gosec // six-digit codes
		}
		out.Picks = append(out.Picks, answer)
	}
	return out, nil
}

func (h *ws) enterPortal(c *transport.Context, _ *clientv1.EnterPortal) (any, error) {
	return nil, worldErr(h.world.EnterPortal(c.Ctx, presence(c)))
}

// teleport answers once the world started the channel; its MapChange or TeleportCancelled comes on the world's stream.
func (h *ws) teleport(c *transport.Context, in *clientv1.Teleport) (any, error) {
	return nil, worldErr(h.world.Teleport(c.Ctx, presence(c), in.GetMapId(), in.GetWaypoint(), c.Message.Seq))
}

// switchChannel answers once the world started the switch; its MapChange or TeleportCancelled comes on the world's stream.
func (h *ws) switchChannel(c *transport.Context, in *clientv1.SwitchChannel) (any, error) {
	return nil, worldErr(h.world.SwitchChannel(c.Ctx, presence(c), int(in.GetChannel()), c.Message.Seq))
}

// channels answers with the region's open channels.
func (h *ws) channels(c *transport.Context, _ *clientv1.Channels) (any, error) {
	list, err := h.world.Channels(c.Ctx, presence(c))
	if err != nil {
		return nil, worldErr(err)
	}
	return &clientv1.Response{Ok: true, Channels: ChannelList(list)}, nil
}

// bosses answers with how each of the region's area bosses stands.
func (h *ws) bosses(c *transport.Context, _ *clientv1.Bosses) (any, error) {
	list, err := h.world.Bosses(c.Ctx, presence(c))
	if err != nil {
		return nil, worldErr(err)
	}
	return &clientv1.Response{Ok: true, Bosses: BossList(list)}, nil
}

// equip answers once the world applied it; the changed slots and stats come on the world's stream, before or after the answer.
func (h *ws) equip(c *transport.Context, in *clientv1.Equip) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, worldErr(h.world.Equip(c.Ctx, presence(c), in.GetIdempotencyKey(), id, in.GetEquipSlot()))
}

func (h *ws) unequip(c *transport.Context, in *clientv1.Unequip) (any, error) {
	var to *int
	if in.ToSlot != nil {
		slot := int(in.GetToSlot())
		to = &slot
	}
	return nil, worldErr(h.world.Unequip(c.Ctx, presence(c), in.GetIdempotencyKey(), in.GetEquipSlot(), to))
}

// useItem answers once the world applied the potion; the changed slot and stats come on the world's stream, before or after the answer.
func (h *ws) useItem(c *transport.Context, in *clientv1.UseItem) (any, error) {
	id, err := kernel.ParseItemID(in.GetItemId())
	if err != nil {
		return nil, err
	}
	return nil, worldErr(h.world.UseItem(c.Ctx, presence(c), in.GetIdempotencyKey(), id, in.GetTemplate()))
}

// setLook answers once the world applied it; the look comes in the player's next view.
func (h *ws) setLook(c *transport.Context, in *clientv1.SetLook) (any, error) {
	l := in.GetLook()
	look := world.Look{Outfit: l.GetOutfit(), Headwear: l.GetHeadwear(), Face: l.GetFace(), Weapon: l.GetWeapon(), Wings: l.GetWings(), HideHeadwear: l.GetHideHeadwear()}
	return nil, worldErr(h.world.SetLook(c.Ctx, presence(c), in.GetIdempotencyKey(), look))
}

// spendPoints answers once the world applied the points; StatsChanged comes on the world's stream, before or after the answer.
func (h *ws) spendPoints(c *transport.Context, in *clientv1.SpendPoints) (any, error) {
	p := in.GetPoints()
	pts := gamedata.Stats{STR: int(p.GetStr()), DEX: int(p.GetDex()), INT: int(p.GetInt()), AGI: int(p.GetAgi()), VIT: int(p.GetVit())}
	return nil, worldErr(h.world.SpendPoints(c.Ctx, presence(c), int(in.GetExpectedUnspent()), pts))
}

func (h *ws) resetPoints(c *transport.Context, in *clientv1.ResetPoints) (any, error) {
	return nil, worldErr(h.world.ResetPoints(c.Ctx, presence(c), int(in.GetExpectedUnspent())))
}

// spendSkillPoints answers once the world applied the skill points; StatsChanged comes on the world's stream, before or after the answer.
func (h *ws) spendSkillPoints(c *transport.Context, in *clientv1.SpendSkillPoints) (any, error) {
	pts := make(map[string]int, len(in.GetPoints()))
	for skill, n := range in.GetPoints() {
		pts[skill] = int(n)
	}
	return nil, worldErr(h.world.SpendSkillPoints(c.Ctx, presence(c), int(in.GetExpectedUnspent()), pts))
}

// chooseTrait answers once the world set the trait; StatsChanged comes on the world's stream, before or after the answer.
func (h *ws) chooseTrait(c *transport.Context, in *clientv1.ChooseTrait) (any, error) {
	return nil, worldErr(h.world.ChooseTrait(c.Ctx, presence(c), in.GetSkill(), in.GetTrait()))
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
	use := world.SkillUse{Skill: in.GetSkill(), Root: in.GetChainRoot(), Aim: aim, Target: target, Way: sim.Way{X: w.GetX(), Z: w.GetZ(), Length: w.GetLength()}}
	return nil, worldErr(h.world.UseSkill(c.Ctx, presence(c), use, c.Message.Seq))
}

func (h *ws) talk(c *transport.Context, in *clientv1.Talk) (any, error) {
	return nil, worldErr(h.world.Talk(c.Ctx, presence(c), in.GetNpcId()))
}

// promote answers once the promotion applied; Promoted comes on the world's stream.
func (h *ws) promote(c *transport.Context, in *clientv1.Promote) (any, error) {
	return nil, worldErr(h.world.Promote(c.Ctx, presence(c), in.GetNpcId(), kernel.Class(in.GetClass())))
}

func (h *ws) acceptQuest(c *transport.Context, in *clientv1.AcceptQuest) (any, error) {
	return nil, worldErr(h.world.AcceptQuest(c.Ctx, presence(c), in.GetNpcId(), in.GetQuestId()))
}

func (h *ws) abandonQuest(c *transport.Context, in *clientv1.AbandonQuest) (any, error) {
	return nil, worldErr(h.world.AbandonQuest(c.Ctx, presence(c), in.GetQuestId()))
}

func (h *ws) handInQuest(c *transport.Context, in *clientv1.HandInQuest) (any, error) {
	return nil, worldErr(h.world.HandInQuest(c.Ctx, presence(c), in.GetNpcId(), in.GetQuestId()))
}

// pvpForfeit takes the player out of their match as a loss; during the outcome it only takes them back. The world holds the matches.
func (h *ws) pvpForfeit(c *transport.Context, _ *clientv1.PvpForfeit) (any, error) {
	return nil, worldErr(h.world.Forfeit(c.Ctx, presence(c)))
}

// targetOf is a lock as the client names it: "m:<mob id>" or "p:<character id>", empty for none.
func targetOf(ref string) (world.EntityRef, error) {
	if ref == "" {
		return world.EntityRef{}, nil
	}
	if id, ok := strings.CutPrefix(ref, "m:"); ok {
		mob, err := strconv.ParseUint(id, 10, 64)
		if err != nil || mob == 0 {
			return world.EntityRef{}, kernel.ErrBadID
		}
		return world.EntityRef{Mob: mob}, nil
	}
	if id, ok := strings.CutPrefix(ref, "p:"); ok {
		character, err := kernel.ParseCharacterID(id)
		if err != nil {
			return world.EntityRef{}, err
		}
		return world.EntityRef{Player: character}, nil
	}
	return world.EntityRef{}, kernel.ErrBadID
}

// aimOf is a yaw an attack or a skill aims along, refused unless finite.
func aimOf(yaw float32) (float32, error) {
	if f := float64(yaw); math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, failure.ErrBadAim
	}
	return yaw, nil
}

// presence is the character a message is for and the connection it came on.
func presence(c *transport.Context) world.Presence {
	s := c.Session.Session()
	return world.Presence{Character: s.Character(), Conn: s.ConnID(), Seq: c.Message.Seq}
}

// worldErr is a world failure as the client sees it: a class the game data lacks is the world's own fault, so internal.
func worldErr(err error) error {
	if errors.Is(err, kernel.ErrClassUnavailable) {
		return &failure.ServiceError{Public: failure.Internal, Err: err}
	}
	return err
}
