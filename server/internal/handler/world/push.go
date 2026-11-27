package world

import (
	"errors"
	"strconv"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/pkg/wsconn"
)

// Encoders registers the encoders of the pushes the module owns.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) { out.Quests = questStates(w.Quests) })
	frames := newFrames()
	return errors.Join(
		transport.Encode(e, func(p model.AreaEntered) any {
			up, down := bossState(p.FieldBoss, p.FieldBossGone)
			return &clientv1.AreaEntered{Area: p.Area, FieldBoss: up, FieldBossGone: down}
		}),
		transport.Encode(e, func(p model.AttackUsed) any {
			return &clientv1.AttackUsed{Attacker: p.Attacker, LengthTicks: uint32(p.LengthTicks), Aim: p.Aim} //nolint:gosec // lengths are small
		}),
		transport.Encode(e, func(p model.Damage) any {
			return &clientv1.Damage{Source: entityRef(p.Source), Target: entityRef(p.Target), Amount: int32(p.Amount), Crit: p.Crit, Miss: p.Miss, Dodge: p.Dodge, Block: p.Block, StatusId: p.Status, Shielded: int32(p.Shielded)}
		}),
		transport.Encode(e, func(p model.Delta) any {
			return wsconn.Spliced{Shared: frames.shared(p.Frame, p.Parts), Own: &clientv1.Delta{AckSeq: p.AckSeq, Players: ownView(p.Own), Energy: p.Energy, Unseen: unseen(p.Unseen), Pouches: pouchViews(p.Pouches), PouchesGone: instanceIDs(p.PouchesGone)}}
		}),
		transport.Encode(e, func(p model.Died) any {
			return &clientv1.Died{CharacterId: p.CharacterID, X: p.X, Y: p.Y, Z: p.Z}
		}),
		transport.Encode(e, func(p model.ExpGained) any {
			return &clientv1.ExpGained{Amount: p.Amount, Exp: p.Exp}
		}),
		transport.Encode(e, func(p model.FieldBoss) any {
			return fieldBoss(p)
		}),
		transport.Encode(e, func(p model.FieldBossGone) any {
			return bossGone(p)
		}),
		transport.Encode(e, func(p model.Loot) any {
			items := make([]*clientv1.LootItem, 0, len(p.Items))
			for _, it := range p.Items {
				items = append(items, &clientv1.LootItem{Template: it.Template, Quantity: int32(it.Quantity)}) //nolint:gosec // a stack's size
			}
			return &clientv1.Loot{Kind: lootKinds[p.Kind], MapId: p.MapID, PouchId: uint32(p.Pouch), OwnerId: p.Owner, OwnerName: p.OwnerName, Items: items} //nolint:gosec // pouch IDs count up within one map instance
		}),
		transport.Encode(e, func(p model.MapChange) any {
			return &clientv1.MapChange{MapId: p.MapID, X: p.X, Y: p.Y, Z: p.Z, Yaw: p.Yaw}
		}),
		transport.Encode(e, func(p model.MobAttacked) any {
			return &clientv1.MobAttacked{Mob: uint32(p.MobID), Attack: uint32(p.Attack), ToX: p.ToX, Yaw: p.Yaw, ToY: p.ToY, ToZ: p.ToZ} //nolint:gosec // small: mob IDs and attack indexes
		}),
		transport.Encode(e, func(p model.NpcDialog) any {
			return npcDialog(p)
		}),
		transport.Encode(e, func(p model.PartySpots) any {
			spots := make([]*clientv1.PartySpot, len(p.Spots))
			for k, s := range p.Spots {
				spots[k] = &clientv1.PartySpot{CharacterId: s.CharacterID, X: float32(s.X), Z: float32(s.Z)}
			}
			return &clientv1.PartySpots{Spots: spots}
		}),
		transport.Encode(e, func(p model.QuestChanged) any {
			return &clientv1.QuestChanged{Change: questChanges[p.Change], Quest: questState(p.Quest)}
		}),
		transport.Encode(e, func(p model.Respawned) any {
			return &clientv1.Respawned{CharacterId: p.CharacterID, MapId: p.MapID, X: p.X, Y: p.Y, Hp: int32(p.HP), Z: p.Z, Yaw: p.Yaw}
		}),
		transport.Encode(e, func(p model.SkillUsed) any {
			return &clientv1.SkillUsed{Caster: p.Caster, Skill: p.Skill, Stage: int32(p.Stage), Trait: p.Trait, Aim: p.Aim, Target: lockRef(p.Target)}
		}),
		transport.Encode(e, func(p model.Snapshot) any {
			snap := &clientv1.Snapshot{Channel: uint32(p.Channel), Area: p.Area, Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), Pouches: pouchViews(p.Pouches), Boss: mobView(p.Boss)}
			snap.FieldBoss, snap.FieldBossGone = bossState(p.FieldBoss, p.FieldBossGone)
			return snap
		}),
		transport.Encode(e, func(p model.TeleportCancelled) any {
			return &clientv1.TeleportCancelled{Reason: teleportCancels[p.Reason]}
		}),
		transport.Encode(e, func(p model.WaypointFound) any {
			return &clientv1.WaypointFound{Waypoint: p.Waypoint}
		}),
	)
}

func playerViews(ps []model.PlayerView) []*clientv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &clientv1.PlayerView{
			Id: p.ID, Slot: p.Slot, Name: p.Name, Class: p.Class, Level: int32(p.Level),
			X: p.X, Y: p.Y, Vx: p.VX, Vy: p.VY,
			OnGround: p.OnGround, Hp: int32(p.HP), MaxHp: int32(p.MaxHP), Dead: p.Dead,
			Invulnerable: p.Invulnerable, Z: p.Z, Vz: p.VZ, Hx: p.HX, Hz: p.HZ, Aim: p.Aim,
			Move: skillMove(p.Move), Dodging: p.Dodging, Shield: int32(p.Shield), //nolint:gosec // a shield is a few hundred
			Held: int32(p.Held), Slow: slowWalk(p.Slow), Statuses: statusViews(p.Statuses), //nolint:gosec // a hold is a few hundred sub-steps
			Team: int32(p.Team), Bot: p.Bot, //nolint:gosec // 0, 1 or 2
			Sprint: int32(p.Sprint), //nolint:gosec // a sprint's count stops at its start, a few hundred sub-steps
			Walk:   p.Walk,
			Look:   lookOf(p.Look),
		})
	}
	return out
}

// ownView is the player's own view as a one-view list, nil when it didn't change.
func ownView(p *model.PlayerView) []*clientv1.PlayerView {
	if p == nil {
		return nil
	}
	return playerViews([]model.PlayerView{*p})
}

func playerSteps(ss []model.PlayerStep) []*clientv1.PlayerStep {
	if len(ss) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerStep, len(ss))
	for i, s := range ss {
		out[i] = &clientv1.PlayerStep{Slot: s.Slot, Dx: s.DX, Dy: s.DY, Dz: s.DZ, Vx: s.VX, Vy: s.VY, Vz: s.VZ, Hx: s.HX, Hz: s.HZ, OnGround: s.OnGround, Hp: s.HP, Sprint: s.Sprint, Held: s.Held, Aim: s.Aim}
	}
	return out
}

func mobSteps(ss []model.MobStep) []*clientv1.MobStep {
	if len(ss) == 0 {
		return nil
	}
	out := make([]*clientv1.MobStep, len(ss))
	for i, s := range ss {
		out[i] = &clientv1.MobStep{Id: uint32(s.ID), Dx: s.DX, Dy: s.DY, Dz: s.DZ, Yaw: s.Yaw, Hp: s.HP} //nolint:gosec // mob IDs are small
		if s.Pose != nil {
			pose := mobPoses[*s.Pose]
			out[i].Pose = &pose
		}
	}
	return out
}

// lookOf is a look on the wire: its picks, as every view carries them; the revision stays on the server, which sends only the newest.
func lookOf(l model.Look) *clientv1.Look {
	return &clientv1.Look{Outfit: l.Outfit, Headwear: l.Headwear, Face: l.Face, Weapon: l.Weapon, HideHeadwear: l.HideHeadwear, Wings: l.Wings}
}

func slowWalk(s model.PlayerSlow) *clientv1.SlowWalk {
	if s.Left == 0 {
		return nil
	}
	return &clientv1.SlowWalk{Left: int32(s.Left), Factor: s.Factor} //nolint:gosec // a slow is a few hundred sub-steps
}

func statusViews(ss []model.StatusView) []*clientv1.StatusView {
	if len(ss) == 0 {
		return nil
	}
	out := make([]*clientv1.StatusView, 0, len(ss))
	for _, s := range ss {
		out = append(out, &clientv1.StatusView{Id: s.ID, Ends: uint32(s.Ends)}) //nolint:gosec // a tick fits for years at 20 Hz
	}
	return out
}

func skillMove(m model.PlayerMove) *clientv1.SkillMove {
	if m.Left == 0 {
		return nil
	}
	return &clientv1.SkillMove{Vx: m.VX, Vz: m.VZ, Left: int32(m.Left), Stop: m.Stop, Through: m.Through, ClearX: m.ClearX, ClearZ: m.ClearZ} //nolint:gosec // a move is a few sub-steps
}

func mobViews(ms []model.MobView) []*clientv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*clientv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, mobViewOf(m))
	}
	return out
}

func mobView(m *model.MobView) *clientv1.MobView {
	if m == nil {
		return nil
	}
	return mobViewOf(*m)
}

func mobViewOf(m model.MobView) *clientv1.MobView {
	return &clientv1.MobView{
		Id: uint32(m.ID), Type: m.Type, X: m.X, Y: m.Y, Hp: int32(m.HP), MaxHp: int32(m.MaxHP), Pose: mobPoses[m.Pose],
		Z: m.Z, Yaw: m.Yaw, Statuses: statusViews(m.Statuses),
	}
}

// unseen is what a player stopped seeing, nil for nothing, so most frames carry no field for it.
func unseen(u *model.Unseen) *clientv1.Unseen {
	if u == nil {
		return nil
	}
	return &clientv1.Unseen{Players: u.Players, Mobs: instanceIDs(u.Mobs)}
}

var mobPoses = map[model.MobPose]clientv1.MobPose{
	model.Standing:  clientv1.MobPose_MOB_POSE_UNSPECIFIED,
	model.Flinching: clientv1.MobPose_MOB_POSE_FLINCH,
	model.Down:      clientv1.MobPose_MOB_POSE_DOWN,
	model.Rising:    clientv1.MobPose_MOB_POSE_RISING,
}

var teleportCancels = map[model.TeleportCancel]clientv1.TeleportCancelReason{
	model.TeleportMoved: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_MOVED,
	model.TeleportActed: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_ACTED,
	model.TeleportHit:   clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT,
	model.TeleportDied:  clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_DIED,
}

func pouchViews(ps []model.PouchView) []*clientv1.PouchView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PouchView, 0, len(ps))
	for _, p := range ps {
		items := make([]*clientv1.PouchItem, 0, len(p.Items))
		for _, it := range p.Items {
			items = append(items, &clientv1.PouchItem{Item: it.ID, Template: it.Template, Quantity: int32(it.Quantity), Stats: transport.ItemStats(it.Stats)}) //nolint:gosec // a stack's size
		}
		out = append(out, &clientv1.PouchView{Id: uint32(p.ID), Kind: pouchKinds[p.Kind], X: p.X, Y: p.Y, Z: p.Z, Items: items, GoneInMs: p.GoneInMs}) //nolint:gosec // pouch IDs count up within one map instance
	}
	return out
}

// instanceIDs are mob or pouch IDs, which count up within one map instance and fit the wire's 32 bits.
func instanceIDs(ids []uint64) []uint32 {
	if len(ids) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		out = append(out, uint32(id))
	}
	return out
}

var lootKinds = map[model.LootKind]clientv1.LootKind{
	model.LootTaken: clientv1.LootKind_LOOT_KIND_TAKEN,
	model.LootLeft:  clientv1.LootKind_LOOT_KIND_LEFT,
}

var pouchKinds = map[model.PouchKind]clientv1.PouchKind{
	model.PouchSmall: clientv1.PouchKind_POUCH_KIND_POUCH,
	model.PouchBox:   clientv1.PouchKind_POUCH_KIND_BOX,
}

func npcDialog(d model.NpcDialog) *clientv1.NpcDialog {
	out := &clientv1.NpcDialog{NpcId: d.NPC}
	for _, o := range d.Gives {
		offer := &clientv1.QuestOffer{QuestId: o.Quest, State: offerStates[o.State]}
		if u := o.Unmet; u.Level != 0 || u.Class != "" || len(u.Quests) > 0 {
			offer.Unmet = &clientv1.QuestConditions{Level: int32(u.Level), Class: u.Class, Quests: u.Quests} //nolint:gosec // a level is small
		}
		out.Gives = append(out.Gives, offer)
	}
	for _, t := range d.Takes {
		out.Takes = append(out.Takes, &clientv1.QuestTurnIn{QuestId: t.Quest, Ready: t.Ready})
	}
	if p := d.Promotion; p != nil {
		out.Promotion = &clientv1.PromotionOffer{Class: p.Class}
		for _, c := range p.Conditions {
			out.Promotion.Conditions = append(out.Promotion.Conditions, &clientv1.PromotionCondition{Kind: conditionKinds[c.Kind], Id: c.ID, Need: int32(c.Need), Have: int32(c.Have)}) //nolint:gosec // conditions are small
		}
	}
	return out
}

var conditionKinds = map[model.ConditionKind]clientv1.PromotionConditionKind{
	model.ConditionLevel: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL,
	model.ConditionStat:  clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT,
	model.ConditionSkill: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL,
	model.ConditionQuest: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST,
}

func questStates(qs []model.QuestState) []*clientv1.QuestState {
	if len(qs) == 0 {
		return nil
	}
	out := make([]*clientv1.QuestState, 0, len(qs))
	for _, q := range qs {
		out = append(out, questState(q))
	}
	return out
}

func questState(q model.QuestState) *clientv1.QuestState {
	var progress []int32
	for _, n := range q.Progress {
		progress = append(progress, int32(n)) //nolint:gosec // a count is at most an objective's
	}
	return &clientv1.QuestState{QuestId: q.Quest, Status: questStatuses[q.Status], Progress: progress, TimesFinished: int32(q.TimesFinished)} //nolint:gosec // it came in as int32
}

var offerStates = map[model.QuestOfferState]clientv1.QuestOfferState{
	model.OfferAvailable: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE,
	model.OfferActive:    clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE,
	model.OfferFinished:  clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED,
	model.OfferUnmet:     clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET,
}

var questStatuses = map[model.QuestStatus]clientv1.QuestStatus{
	model.QuestActive:   clientv1.QuestStatus_QUEST_STATUS_ACTIVE,
	model.QuestFinished: clientv1.QuestStatus_QUEST_STATUS_FINISHED,
}

var questChanges = map[model.QuestChange]clientv1.QuestChange{
	model.QuestProgressed: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED,
	model.QuestAccepted:   clientv1.QuestChange_QUEST_CHANGE_ACCEPTED,
	model.QuestHandedIn:   clientv1.QuestChange_QUEST_CHANGE_HANDED_IN,
	model.QuestAbandoned:  clientv1.QuestChange_QUEST_CHANGE_ABANDONED,
}

// lockRef is a lock as the client names it, empty for none.
func lockRef(e model.Entity) string {
	if e == (model.Entity{}) {
		return ""
	}
	return entityRef(e)
}

// entityRef is how the client names a player or a mob: p:<character id> or m:<mob id>.
func entityRef(e model.Entity) string {
	if e.Character != "" {
		return "p:" + e.Character
	}
	return "m:" + strconv.FormatUint(e.Mob, 10)
}

// bossState is a field boss up or beaten on the wire; nil for neither.
func bossState(up *model.FieldBoss, down *model.FieldBossGone) (*clientv1.FieldBoss, *clientv1.FieldBossGone) {
	var u *clientv1.FieldBoss
	var d *clientv1.FieldBossGone
	if up != nil {
		u = fieldBoss(*up)
	}
	if down != nil {
		d = bossGone(*down)
	}
	return u, d
}

func fieldBoss(b model.FieldBoss) *clientv1.FieldBoss {
	return &clientv1.FieldBoss{MobId: uint32(b.MobID), Type: b.Type}
}

func bossGone(b model.FieldBossGone) *clientv1.FieldBossGone {
	return &clientv1.FieldBossGone{MobId: uint32(b.MobID), Type: b.Type, BackInMs: b.BackInMs}
}

// BossList is each of a region's area bosses: up, or its time back.
func BossList(bosses []world.AreaBoss) *clientv1.BossList {
	out := &clientv1.BossList{}
	for _, b := range bosses {
		out.Bosses = append(out.Bosses, &clientv1.AreaBoss{Area: b.Area, Up: b.Up, BackInMs: b.BackIn.Milliseconds()})
	}
	return out
}

// ChannelList is a region's open channels and how full each is.
func ChannelList(c world.Channels) *clientv1.ChannelList {
	out := &clientv1.ChannelList{Cap: uint32(c.Cap)}
	for _, l := range c.Open {
		out.Channels = append(out.Channels, &clientv1.ChannelLoad{Channel: uint32(l.Channel), Players: uint32(l.Players)})
	}
	return out
}
