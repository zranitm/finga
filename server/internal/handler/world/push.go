package world

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/world"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
)

// Encoders registers the encoders of the world's events, which the world's stream brings, and the Welcome's quests.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		v, ok := w.Joined.(world.View)
		if !ok {
			return
		}
		out.Character, out.SkillTimers, out.Quests = characterView(v), timersLeft(v.Timers, w.ServerTime), questLog(v.Quests)
	})
	frames := newFrames()
	return errors.Join(
		transport.Encode(e, func(p world.AreaEntered) any { return areaEntered(p) }),
		transport.Encode(e, func(p world.ArenaBar) any { return arenaBar(p) }),
		transport.Encode(e, func(p world.AttackUsed) any { return attackUsed(p) }),
		transport.Encode(e, func(p world.Chat) any { return chat(p) }),
		transport.Encode(e, func(p world.Damage) any { return damage(p) }),
		transport.Encode(e, func(p world.Died) any { return died(p) }),
		transport.Encode(e, func(p world.ExpGained) any { return expGained(p) }),
		transport.Encode(e, func(p world.FieldBoss) any { return fieldBoss(p) }),
		transport.Encode(e, func(p world.FieldBossGone) any { return bossGone(p) }),
		transport.Encode(e, func(p world.Framed) any {
			return wsconn.Spliced{Shared: frames.shared(p.Frame, p.Recipient.Parts), Own: recipientDelta(p.Recipient)}
		}),
		transport.Encode(e, func(p world.InventoryChanged) any { return inventoryChanged(p) }),
		transport.Encode(e, func(p world.LevelUp) any { return levelUp(p) }),
		transport.Encode(e, func(p world.Loot) any { return loot(p) }),
		transport.Encode(e, func(p world.MatchState) any { return matchState(p) }),
		transport.Encode(e, func(p world.MobAttacked) any { return mobAttacked(p) }),
		transport.Encode(e, func(p world.NpcDialog) any { return npcDialog(p) }),
		transport.Encode(e, func(p world.PartySpots) any { return partySpots(p) }),
		transport.Encode(e, func(p world.PiecesAdded) any { return piecesAdded(p) }),
		transport.Encode(e, func(p world.Promoted) any { return promoted(p) }),
		transport.Encode(e, func(p world.QuestChanged) any { return questChanged(p) }),
		transport.Encode(e, func(p world.Respawned) any { return respawned(p) }),
		transport.Encode(e, func(p world.SkillUsed) any { return skillUsed(p) }),
		transport.Encode(e, func(p world.Snapshot) any { return snapshot(p) }),
		transport.Encode(e, func(p world.StatsChanged) any { return statsChanged(p) }),
		transport.Encode(e, func(p world.Teleport) any { return teleport(p) }),
		transport.Encode(e, func(p world.TeleportCancelled) any { return teleportCancelled(p) }),
		transport.Encode(e, func(p world.WaypointFound) any { return waypointFound(p) }),
	)
}

func playerViews(ps []world.PlayerView) []*clientv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		st := p.State
		out = append(out, &clientv1.PlayerView{
			Id: p.ID.String(), Slot: p.Slot, Name: p.Name, Class: p.Class.String(), Level: int32(p.Level), //nolint:gosec // a level is small
			X: st.X, Y: st.Y, Vx: st.VX, Vy: st.VY,
			OnGround: st.OnGround, Hp: int32(p.HP), MaxHp: int32(p.MaxHP), Dead: p.Dead, //nolint:gosec // HP fits
			Invulnerable: p.Invulnerable, Z: st.Z, Vz: st.VZ, Hx: st.HX, Hz: st.HZ, Aim: p.Aim,
			Move: skillMove(st.Move), Dodging: p.Dodging, Shield: int32(p.Shield), //nolint:gosec // a shield is a few hundred
			Held: int32(st.Held), Slow: slowWalk(st.Slow), Statuses: statusViews(p.Statuses), //nolint:gosec // a hold is a few hundred sub-steps
			Team: int32(p.Team), Bot: p.Bot, //nolint:gosec // 0, 1 or 2
			Sprint: int32(st.Sprint), //nolint:gosec // a sprint's count stops at its start, a few hundred sub-steps
			Walk:   st.Walk,
			Look:   lookOf(p.Look),
		})
	}
	return out
}

// ownView is the player's own view as a one-view list, nil when it didn't change.
func ownView(p *world.PlayerView) []*clientv1.PlayerView {
	if p == nil {
		return nil
	}
	return playerViews([]world.PlayerView{*p})
}

func playerSteps(ss []world.PlayerStep) []*clientv1.PlayerStep {
	if len(ss) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerStep, len(ss))
	for i, s := range ss {
		out[i] = &clientv1.PlayerStep{Slot: s.Slot, Dx: s.DX, Dy: s.DY, Dz: s.DZ, Vx: s.VX, Vy: s.VY, Vz: s.VZ, Hx: s.HX, Hz: s.HZ, OnGround: s.OnGround, Hp: s.HP, Sprint: s.Sprint, Held: s.Held, Aim: s.Aim}
	}
	return out
}

func mobSteps(ss []world.MobStep) []*clientv1.MobStep {
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
func lookOf(l world.Look) *clientv1.Look {
	return &clientv1.Look{Outfit: l.Outfit, Headwear: l.Headwear, Face: l.Face, Weapon: l.Weapon, HideHeadwear: l.HideHeadwear, Wings: l.Wings}
}

func slowWalk(s sim.Slow) *clientv1.SlowWalk {
	if s.Left == 0 {
		return nil
	}
	return &clientv1.SlowWalk{Left: int32(s.Left), Factor: s.Factor} //nolint:gosec // a slow is a few hundred sub-steps
}

func statusViews(ss []world.StatusView) []*clientv1.StatusView {
	if len(ss) == 0 {
		return nil
	}
	out := make([]*clientv1.StatusView, 0, len(ss))
	for _, s := range ss {
		out = append(out, &clientv1.StatusView{Id: s.ID, Ends: uint32(s.Ends)}) //nolint:gosec // a tick fits for years at 20 Hz
	}
	return out
}

func skillMove(m sim.Move) *clientv1.SkillMove {
	if m.Left == 0 {
		return nil
	}
	return &clientv1.SkillMove{Vx: m.VX, Vz: m.VZ, Left: int32(m.Left), Stop: m.Stop, Through: m.Through, ClearX: m.ClearX, ClearZ: m.ClearZ} //nolint:gosec // a move is a few sub-steps
}

func mobViews(ms []world.MobView) []*clientv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*clientv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, mobViewOf(m))
	}
	return out
}

func mobView(m *world.MobView) *clientv1.MobView {
	if m == nil {
		return nil
	}
	return mobViewOf(*m)
}

func mobViewOf(m world.MobView) *clientv1.MobView {
	return &clientv1.MobView{
		Id: uint32(m.ID), Type: m.Type, X: m.X, Y: m.Y, Hp: int32(m.HP), MaxHp: int32(m.MaxHP), Pose: mobPoses[m.Pose], //nolint:gosec // mob IDs are small
		Z: m.Z, Yaw: m.Yaw, Statuses: statusViews(m.Statuses),
	}
}

// unseen is what a player stopped seeing, nil for nothing, so most frames carry no field for it.
func unseen(u *world.Unseen) *clientv1.Unseen {
	if u == nil {
		return nil
	}
	return &clientv1.Unseen{Players: ids(u.Players), Mobs: instanceIDs(u.Mobs)}
}

// ids are character IDs as the wire names them, nil for none.
func ids(cs []kernel.CharacterID) []string {
	if len(cs) == 0 {
		return nil
	}
	out := make([]string, len(cs))
	for i, id := range cs {
		out[i] = id.String()
	}
	return out
}

var mobPoses = map[world.MobPose]clientv1.MobPose{
	world.Standing:  clientv1.MobPose_MOB_POSE_UNSPECIFIED,
	world.Flinching: clientv1.MobPose_MOB_POSE_FLINCH,
	world.Down:      clientv1.MobPose_MOB_POSE_DOWN,
	world.Rising:    clientv1.MobPose_MOB_POSE_RISING,
}

var teleportCancels = map[world.TeleportCancel]clientv1.TeleportCancelReason{
	world.TeleportMoved: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_MOVED,
	world.TeleportActed: clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_ACTED,
	world.TeleportHit:   clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT,
	world.TeleportDied:  clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_DIED,
}

var pieceCauses = map[world.PieceCause]string{
	world.PieceFromQuest: "quest",
	world.PieceFromFeat:  "feat",
}

func pouchViews(ps []world.PouchView) []*clientv1.PouchView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PouchView, 0, len(ps))
	for _, p := range ps {
		items := make([]*clientv1.PouchItem, 0, len(p.Items))
		for _, it := range p.Items {
			items = append(items, &clientv1.PouchItem{Item: it.ID, Template: it.Template, Quantity: int32(it.Quantity), Stats: transport.ItemStats(it.Stats)}) //nolint:gosec // a stack's size
		}
		out = append(out, &clientv1.PouchView{Id: uint32(p.ID), Kind: pouchKinds[p.Kind], X: p.X, Y: p.Y, Z: p.Z, Items: items, GoneInMs: p.GoneIn.Milliseconds()}) //nolint:gosec // pouch IDs count up within one map instance
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
		out = append(out, uint32(id)) //nolint:gosec // see above
	}
	return out
}

var lootKinds = map[world.LootKind]clientv1.LootKind{
	world.LootTaken: clientv1.LootKind_LOOT_KIND_TAKEN,
	world.LootLeft:  clientv1.LootKind_LOOT_KIND_LEFT,
}

var pouchKinds = map[world.PouchKind]clientv1.PouchKind{
	world.PouchSmall: clientv1.PouchKind_POUCH_KIND_POUCH,
	world.PouchBox:   clientv1.PouchKind_POUCH_KIND_BOX,
}

func npcDialog(d world.NpcDialog) *clientv1.NpcDialog {
	out := &clientv1.NpcDialog{NpcId: d.NPC}
	for _, o := range d.Gives {
		offer := &clientv1.QuestOffer{QuestId: o.Quest, State: offerStates[o.State]}
		if o.State == world.OfferUnmet {
			offer.Unmet = &clientv1.QuestConditions{Level: int32(o.Unmet.Level), Class: o.Unmet.Class.String(), Quests: o.Unmet.Quests} //nolint:gosec // a level is small
		}
		out.Gives = append(out.Gives, offer)
	}
	for _, t := range d.Takes {
		out.Takes = append(out.Takes, &clientv1.QuestTurnIn{QuestId: t.Quest, Ready: t.Ready})
	}
	if p := d.Promotion; p != nil {
		out.Promotion = &clientv1.PromotionOffer{Class: p.Class.String()}
		for _, c := range p.Conditions {
			out.Promotion.Conditions = append(out.Promotion.Conditions, &clientv1.PromotionCondition{Kind: conditionKinds[c.Kind], Id: c.ID, Need: int32(c.Need), Have: int32(c.Have)}) //nolint:gosec // conditions are small
		}
	}
	return out
}

var conditionKinds = map[world.ConditionKind]clientv1.PromotionConditionKind{
	world.ConditionLevel: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL,
	world.ConditionStat:  clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT,
	world.ConditionSkill: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL,
	world.ConditionQuest: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST,
}

// questProto is one quest of the log after a change; a quest that left it has no status.
func questProto(id string, q world.Quest) *clientv1.QuestState {
	return &clientv1.QuestState{QuestId: id, Status: questStatuses[q.Status], Progress: progress(q.Progress), TimesFinished: int32(q.TimesFinished)} //nolint:gosec // it came in as int32
}

func progress(counts []int) []int32 {
	var out []int32
	for _, n := range counts {
		out = append(out, int32(n)) //nolint:gosec // a count is at most an objective's
	}
	return out
}

var offerStates = map[world.OfferState]clientv1.QuestOfferState{
	world.OfferAvailable: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE,
	world.OfferActive:    clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE,
	world.OfferFinished:  clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED,
	world.OfferUnmet:     clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET,
}

var questStatuses = map[world.QuestStatus]clientv1.QuestStatus{
	world.QuestActive:   clientv1.QuestStatus_QUEST_STATUS_ACTIVE,
	world.QuestFinished: clientv1.QuestStatus_QUEST_STATUS_FINISHED,
}

var questChanges = map[world.QuestChange]clientv1.QuestChange{
	world.QuestProgressed: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED,
	world.QuestAccepted:   clientv1.QuestChange_QUEST_CHANGE_ACCEPTED,
	world.QuestHandedIn:   clientv1.QuestChange_QUEST_CHANGE_HANDED_IN,
	world.QuestAbandoned:  clientv1.QuestChange_QUEST_CHANGE_ABANDONED,
}

// lockRef is a lock as the client names it, empty for none.
func lockRef(r world.EntityRef) string {
	if r == (world.EntityRef{}) {
		return ""
	}
	return entityRef(r)
}

// entityRef is how the client names a player or a mob: p:<character id> or m:<mob id>.
func entityRef(r world.EntityRef) string {
	if r.IsMob() {
		return "m:" + strconv.FormatUint(r.Mob, 10)
	}
	return "p:" + r.Player.String()
}

// bossState is a field boss up or beaten on the wire; nil for neither.
func bossState(up *world.FieldBoss, down *world.FieldBossGone) (*clientv1.FieldBoss, *clientv1.FieldBossGone) {
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

func fieldBoss(b world.FieldBoss) *clientv1.FieldBoss {
	return &clientv1.FieldBoss{MobId: uint32(b.Mob), Type: b.Type} //nolint:gosec // mob IDs are small
}

func bossGone(b world.FieldBossGone) *clientv1.FieldBossGone {
	return &clientv1.FieldBossGone{MobId: uint32(b.Mob), Type: b.Type, BackInMs: b.BackIn.Milliseconds()} //nolint:gosec // mob IDs are small
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
	out := &clientv1.ChannelList{Cap: uint32(c.Cap)} //nolint:gosec // a cap is small
	for _, l := range c.Open {
		out.Channels = append(out.Channels, &clientv1.ChannelLoad{Channel: uint32(l.Channel), Players: uint32(l.Players)}) //nolint:gosec // small
	}
	return out
}

// inventoryChanged is the slots and equip slots a change touched.
func inventoryChanged(ic world.InventoryChanged) *clientv1.InventoryChanged {
	slots := make([]*clientv1.InventorySlot, 0, len(ic.Slots))
	for _, s := range ic.Slots {
		slot := &clientv1.InventorySlot{Tab: string(s.Tab), Slot: int32(s.Slot)} //nolint:gosec // a slot is small
		if s.Item != nil {
			slot.Item = slotItem(s.Item, string(s.Tab), s.Slot)
		}
		slots = append(slots, slot)
	}
	var equipment []*clientv1.EquipSlot
	for _, s := range ic.Equipment {
		slot := &clientv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = slotItem(s.Item, "", 0)
		}
		equipment = append(equipment, slot)
	}
	return &clientv1.InventoryChanged{Slots: slots, Revision: ic.Revision, Equipment: equipment}
}

func slotItem(it *world.SlotItem, tab string, slot int) *clientv1.ItemView {
	return &clientv1.ItemView{ItemId: it.ID.String(), Template: it.Template, Tab: tab, Slot: int32(slot), Quantity: int32(it.Quantity), Stats: transport.ItemStats(it.Stats)} //nolint:gosec // a slot and a stack are small
}

// statsChanged is the player's HP, and the sheet or the timers when they changed; the time left on each timer is as of now.
func statsChanged(sc world.StatsChanged) *clientv1.StatsChanged {
	out := &clientv1.StatsChanged{Hp: int32(sc.HP), MaxHp: int32(sc.MaxHP)} //nolint:gosec // HP fits
	if sc.Sheet != nil {
		out.Sheet = sheet(*sc.Sheet)
	}
	if sc.Timers != nil {
		out.Timers = &clientv1.SkillTimers{Timers: timersLeft(*sc.Timers, time.Now())}
	}
	return out
}

// timersLeft is the time left at now, to the millisecond, on each running cooldown, then each buff, by skill.
func timersLeft(t world.SkillTimers, now time.Time) []*clientv1.SkillTimer {
	out := make([]*clientv1.SkillTimer, 0, len(t.Cooldowns)+len(t.Buffs))
	for _, s := range slices.Sorted(maps.Keys(t.Cooldowns)) {
		if left := t.Cooldowns[s].Sub(now); left > 0 {
			out = append(out, &clientv1.SkillTimer{Skill: s, Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: left.Truncate(time.Millisecond).Milliseconds()})
		}
	}
	for _, s := range slices.Sorted(maps.Keys(t.Buffs)) {
		if b := t.Buffs[s]; b.Until.After(now) {
			out = append(out, &clientv1.SkillTimer{Skill: s, Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: b.Until.Sub(now).Truncate(time.Millisecond).Milliseconds(), Points: int32(b.Points), Trait: b.Trait}) //nolint:gosec // points are small
		}
	}
	return out
}

// leveledView is the player's character as a level-up or a promotion leaves it; its energy comes with the frames.
func leveledView(ev world.LevelUp) *clientv1.CharacterView {
	return &clientv1.CharacterView{
		Id: ev.ID.String(), Name: ev.Name, Class: ev.Class.String(), Level: int32(ev.Level), Exp: ev.Exp, Stats: stats(ev.Stats), //nolint:gosec // a level is small
		Hp: int32(ev.HP), MaxHp: int32(ev.MaxHP), Sheet: sheet(ev.Sheet), //nolint:gosec // HP fits
	}
}

func stats(s gamedata.Stats) *clientv1.Stats {
	return &clientv1.Stats{Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT)} //nolint:gosec // stats are small
}

func sheet(s world.Sheet) *clientv1.StatSheet {
	return &clientv1.StatSheet{
		Primary: stats(s.Primary), PrimaryWorn: stats(s.PrimaryWorn), Derived: derived(s.Derived), DerivedWorn: derived(s.DerivedWorn),
		PrimarySkills: stats(s.PrimarySkills), DerivedSkills: derived(s.DerivedSkills), Skills: skillPoints(s.Skills),
		Points:            &clientv1.Points{Unspent: int32(s.Points.Unspent), Spent: stats(s.Points.Spent), FreeResetLeft: !s.Points.FreeResetUsed}, //nolint:gosec // points are small
		AttackLengthTicks: uint32(s.AttackLengthTicks),                                                                                              //nolint:gosec // an attack is a few ticks
		Buffs:             buffViews(s.Buffs),
	}
}

func buffViews(bs []world.BuffView) []*clientv1.BuffView {
	out := make([]*clientv1.BuffView, 0, len(bs))
	for _, b := range bs {
		v := &clientv1.BuffView{Skill: b.Skill, LeftMs: b.Left.Milliseconds(), Points: int32(b.Points)} //nolint:gosec // points are small
		if !b.From.IsZero() {
			v.From = b.From.String()
		}
		out = append(out, v)
	}
	return out
}

func skillPoints(s world.Skills) *clientv1.SkillPoints {
	out := &clientv1.SkillPoints{Unspent: int32(s.Unspent), Invested: make(map[string]int32, len(s.Points)), Traits: maps.Clone(s.Traits)} //nolint:gosec // points are small
	for skill, n := range s.Points {
		out.Invested[skill] = int32(n) //nolint:gosec // points are small
	}
	return out
}

func derived(d progression.Derived) *clientv1.DerivedStats {
	return &clientv1.DerivedStats{
		PhysicalAttack: d.PhysicalAttack, MagicAttack: d.MagicAttack, PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense,
		Accuracy: d.Accuracy, Evasion: d.Evasion, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration,
		HpRegen: d.HPRegen, CooldownSpeed: d.CooldownSpeed, MoveSpeed: d.MoveSpeed,
		MaxHp: int32(d.MaxHP), //nolint:gosec // HP fits
	}
}

var matchPhases = map[world.MatchPhase]clientv1.MatchPhase{
	world.PhasePrep:      clientv1.MatchPhase_MATCH_PHASE_PREP,
	world.PhaseCountdown: clientv1.MatchPhase_MATCH_PHASE_COUNTDOWN,
	world.PhaseFight:     clientv1.MatchPhase_MATCH_PHASE_FIGHT,
	world.PhaseBetween:   clientv1.MatchPhase_MATCH_PHASE_BETWEEN,
	world.PhaseResult:    clientv1.MatchPhase_MATCH_PHASE_RESULT,
}

// matchState is a match as its fighters see it.
func matchState(st world.MatchState) *clientv1.MatchState {
	out := &clientv1.MatchState{
		Match: st.Match.String(), Mode: st.Mode, Phase: matchPhases[st.Phase], Round: int32(st.Round), //nolint:gosec // a few rounds
		EndsInMs: uint32(st.EndsIn.Milliseconds()), //nolint:gosec // minutes at most
	}
	if st.Battle {
		out.Score = []int32{int32(st.Score[0]), int32(st.Score[1])} //nolint:gosec // up to the goal
		for _, p := range st.Points {
			out.Points = append(out.Points, &clientv1.CapturePoint{Id: p.ID, Holder: int32(p.Holder), Progress: p.Progress, Contested: p.Contested}) //nolint:gosec // 0 to 2
		}
	} else {
		out.Wins = []int32{int32(st.Wins[0]), int32(st.Wins[1])} //nolint:gosec // a few rounds
	}
	for _, side := range st.Sides {
		s := &clientv1.MatchSide{}
		for _, f := range side {
			s.Fighters = append(s.Fighters, &clientv1.MatchFighter{Id: f.ID.String(), Name: f.Name, Class: f.Class.String(), Bot: f.Bot, Away: f.Away})
		}
		out.Sides = append(out.Sides, s)
	}
	return out
}

func areaEntered(p world.AreaEntered) *clientv1.AreaEntered {
	up, down := bossState(p.FieldBoss, p.FieldBossGone)
	return &clientv1.AreaEntered{Area: p.Area, FieldBoss: up, FieldBossGone: down}
}

func arenaBar(p world.ArenaBar) *clientv1.ArenaBar { return &clientv1.ArenaBar{SkillBar: p.Bar} }

func attackUsed(p world.AttackUsed) *clientv1.AttackUsed {
	return &clientv1.AttackUsed{Attacker: p.Attacker.String(), LengthTicks: uint32(p.Length), Aim: p.Aim} //nolint:gosec // lengths are small
}

func chat(p world.Chat) *clientv1.ChatMessage {
	return &clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: p.Sender.String(), SenderName: p.SenderName, Text: p.Text}
}

func damage(p world.Damage) *clientv1.Damage {
	return &clientv1.Damage{Source: entityRef(p.Source), Target: entityRef(p.Target), Amount: int32(p.Amount), Crit: p.Crit, Miss: p.Miss, Dodge: p.Dodge, Block: p.Block, StatusId: p.Status, Shielded: int32(p.Shielded)} //nolint:gosec // damage fits
}

func died(p world.Died) *clientv1.Died {
	return &clientv1.Died{CharacterId: p.ID.String(), X: p.X, Y: p.Y, Z: p.Z}
}

func expGained(p world.ExpGained) *clientv1.ExpGained {
	return &clientv1.ExpGained{Amount: p.Amount, Exp: p.Exp}
}

func levelUp(p world.LevelUp) *clientv1.LevelUp { return &clientv1.LevelUp{Character: leveledView(p)} }

func loot(p world.Loot) *clientv1.Loot {
	items := make([]*clientv1.LootItem, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, &clientv1.LootItem{Template: it.Template, Quantity: int32(it.Quantity)}) //nolint:gosec // a stack's size
	}
	return &clientv1.Loot{Kind: lootKinds[p.Kind], MapId: p.MapID, PouchId: uint32(p.Pouch), OwnerId: p.Owner.String(), OwnerName: p.OwnerName, Items: items} //nolint:gosec // pouch IDs count up within one map instance
}

func mobAttacked(p world.MobAttacked) *clientv1.MobAttacked {
	return &clientv1.MobAttacked{Mob: uint32(p.Mob), Attack: uint32(p.Attack), ToX: p.ToX, Yaw: p.Yaw, ToY: p.ToY, ToZ: p.ToZ} //nolint:gosec // small: mob IDs and attack indexes
}

func partySpots(p world.PartySpots) *clientv1.PartySpots {
	spots := make([]*clientv1.PartySpot, len(p.Spots))
	for k, s := range p.Spots {
		spots[k] = &clientv1.PartySpot{CharacterId: s.ID.String(), X: float32(s.X), Z: float32(s.Z)}
	}
	return &clientv1.PartySpots{Spots: spots}
}

func piecesAdded(p world.PiecesAdded) *clientv1.PiecesAdded {
	pieces := make([]*clientv1.AddedPiece, 0, len(p.Pieces))
	for _, id := range p.Pieces {
		pieces = append(pieces, &clientv1.AddedPiece{Id: id, Cause: pieceCauses[p.Cause]})
	}
	return &clientv1.PiecesAdded{Pieces: pieces}
}

func promoted(p world.Promoted) *clientv1.Promoted {
	return &clientv1.Promoted{Character: leveledView(world.LevelUp(p))}
}

func questChanged(p world.QuestChanged) *clientv1.QuestChanged {
	return &clientv1.QuestChanged{Change: questChanges[p.Change], Quest: questProto(p.ID, p.Quest)}
}

func respawned(p world.Respawned) *clientv1.Respawned {
	return &clientv1.Respawned{CharacterId: p.ID.String(), MapId: p.MapID, X: p.X, Y: p.Y, Hp: int32(p.HP), Z: p.Z, Yaw: p.Yaw}
}

func skillUsed(p world.SkillUsed) *clientv1.SkillUsed {
	return &clientv1.SkillUsed{Caster: p.Caster.String(), Skill: p.Skill, Stage: int32(p.Stage), Trait: p.Trait, Aim: p.Aim, Target: lockRef(p.Target)} //nolint:gosec // a stage is small
}

func snapshot(p world.Snapshot) *clientv1.Snapshot {
	snap := &clientv1.Snapshot{Channel: uint32(p.Channel), Area: p.Area, Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), Pouches: pouchViews(p.Pouches), Boss: mobView(p.Boss)} //nolint:gosec // a channel is small
	snap.FieldBoss, snap.FieldBossGone = bossState(p.FieldBoss, p.FieldBossGone)
	return snap
}

func teleport(p world.Teleport) *clientv1.MapChange {
	return &clientv1.MapChange{MapId: p.MapID, X: p.X, Y: p.Y, Z: p.Z, Yaw: p.Yaw}
}

func teleportCancelled(p world.TeleportCancelled) *clientv1.TeleportCancelled {
	return &clientv1.TeleportCancelled{Reason: teleportCancels[p.Reason]}
}

func waypointFound(p world.WaypointFound) *clientv1.WaypointFound {
	return &clientv1.WaypointFound{Waypoint: p.Waypoint}
}

// recipientDelta is what a frame carries for one player alone: their input acknowledged, their energy, what they stopped seeing, their own view and pouches.
func recipientDelta(r world.Recipient) *clientv1.Delta {
	var energy *uint32
	if r.Energy != nil {
		v := uint32(*r.Energy) //nolint:gosec // energy is 0 to the data's max
		energy = &v
	}
	return &clientv1.Delta{AckSeq: r.AckSeq, Players: ownView(r.Own), Energy: energy, Unseen: unseen(r.Unseen), Pouches: pouchViews(r.Pouches.Changed), PouchesGone: instanceIDs(r.Pouches.Gone)}
}

// partDelta is what a frame's part carries for every player sent it.
func partDelta(p world.Part) *clientv1.Delta {
	return &clientv1.Delta{
		Players: playerViews(p.Players), Steps: playerSteps(p.Steps), Mobs: mobViews(p.Mobs), MobSteps: mobSteps(p.MobSteps),
		RemovedPlayers: ids(p.Removed), RemovedMobs: instanceIDs(p.RemovedMobs), Boss: mobView(p.Boss),
	}
}

// characterView is the player's own character as it joins; its energy comes now, each frame carrying it after.
func characterView(v world.View) *clientv1.CharacterView {
	return &clientv1.CharacterView{
		Id: v.ID.String(), Name: v.Name, Class: v.Class.String(), Level: int32(v.Level), Exp: v.Exp, Stats: stats(v.Stats), //nolint:gosec // a level is small
		Hp: int32(v.HP), MaxHp: int32(v.MaxHP), Energy: uint32(v.Energy), Sheet: sheet(v.Sheet), //nolint:gosec // HP fits, and energy is 0 to the data's max
	}
}

// questLog is the log by quest ID, nil when empty.
func questLog(log map[string]world.Quest) []*clientv1.QuestState {
	if len(log) == 0 {
		return nil
	}
	out := make([]*clientv1.QuestState, 0, len(log))
	for _, id := range slices.Sorted(maps.Keys(log)) {
		out = append(out, questProto(id, log[id]))
	}
	return out
}
