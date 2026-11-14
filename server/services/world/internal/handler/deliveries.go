package handler

import (
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/services/world"
)

// deliverResponse is one delivery as world.v1 has it: a player's event names the player and its connection, a frame its recipients; nil for no event.
func deliverResponse(d world.Delivery) *worldv1.DeliverResponse {
	m := &worldv1.DeliverResponse{Tick: d.Tick}
	if !d.Character.IsZero() {
		m.CharacterId, m.ConnectionId = d.Character.String(), d.Conn
	}
	switch ev := d.Event.(type) {
	case world.Frame:
		recipients := make([]*worldv1.Recipient, 0, len(ev.Recipients))
		for _, r := range ev.Recipients {
			rec := &worldv1.Recipient{CharacterId: r.Character.String(), ConnectionId: r.Connection, AckSeq: r.AckSeq}
			if r.Energy != nil {
				rec.Energy = new(uint32(*r.Energy)) //nolint:gosec // energy is 0 to the data's max
			}
			recipients = append(recipients, rec)
		}
		m.Event = &worldv1.DeliverResponse_Frame{Frame: &worldv1.Frame{Delta: encodeFrame(ev), Recipients: recipients}}
	case world.Snapshot:
		snap := &worldv1.Snapshot{MapId: ev.MapID, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Drops: dropViews(ev.Drops)}
		if ev.FieldBoss != nil {
			snap.FieldBoss = fieldBoss(*ev.FieldBoss)
		}
		m.Event = &worldv1.DeliverResponse_Snapshot{Snapshot: snap}
	case world.FieldBoss:
		m.Event = &worldv1.DeliverResponse_FieldBoss{FieldBoss: fieldBoss(ev)}
	case world.FieldBossGone:
		m.Event = &worldv1.DeliverResponse_FieldBossGone{FieldBossGone: &worldv1.FieldBossGone{MobId: ev.Mob, Beaten: ev.Beaten}}
	case world.Teleport:
		m.Event = &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: ev.MapID, X: ev.X, Y: ev.Y}}
	case world.Damage:
		m.Event = &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{
			Source: entityRef(ev.Source), Target: entityRef(ev.Target), Amount: int32(ev.Amount), Crit: ev.Crit, Miss: ev.Miss,
		}}
	case world.SkillUsed:
		m.Event = &worldv1.DeliverResponse_SkillUsed{SkillUsed: &worldv1.SkillUsed{
			Caster: ev.Caster.String(), Skill: ev.Skill, Stage: int32(ev.Stage), Facing: int32(ev.Facing),
		}}
	case world.AttackUsed:
		m.Event = &worldv1.DeliverResponse_AttackUsed{AttackUsed: &worldv1.AttackUsed{
			Attacker: ev.Attacker.String(), Facing: int32(ev.Facing), LengthTicks: uint32(ev.Length), //nolint:gosec // lengths are small
		}}
	case world.MobAttacked:
		m.Event = &worldv1.DeliverResponse_MobAttacked{MobAttacked: &worldv1.MobAttacked{
			Mob: ev.Mob, Attack: uint32(ev.Attack), Facing: int32(ev.Facing), //nolint:gosec // a mob has a few attacks
		}}
	case world.Died:
		m.Event = &worldv1.DeliverResponse_Died{Died: &worldv1.Died{CharacterId: ev.ID.String(), X: ev.X, Y: ev.Y}}
	case world.Respawned:
		m.Event = &worldv1.DeliverResponse_Respawned{Respawned: &worldv1.Respawned{
			CharacterId: ev.ID.String(), MapId: ev.MapID, X: ev.X, Y: ev.Y, Hp: int32(ev.HP),
		}}
	case world.ExpGained:
		m.Event = &worldv1.DeliverResponse_ExpGained{ExpGained: &worldv1.ExpGained{Amount: ev.Amount, Exp: ev.Exp}}
	case world.LevelUp:
		m.Event = &worldv1.DeliverResponse_LevelUp{LevelUp: &worldv1.LevelUp{Character: leveledView(ev)}}
	case world.Promoted:
		m.Event = &worldv1.DeliverResponse_Promoted{Promoted: &worldv1.Promoted{Character: leveledView(world.LevelUp(ev))}}
	case world.StatsChanged:
		sc := &worldv1.StatsChanged{Hp: int32(ev.HP), MaxHp: int32(ev.MaxHP)}
		if ev.Sheet != nil {
			sc.Sheet = statSheet(*ev.Sheet)
		}
		m.Event = &worldv1.DeliverResponse_StatsChanged{StatsChanged: sc}
	case world.Chat:
		m.Event = &worldv1.DeliverResponse_Chat{Chat: &worldv1.Chat{SenderId: ev.Sender.String(), SenderName: ev.SenderName, Text: ev.Text}}
	case world.InventoryChanged:
		m.Event = &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
			Slots: slots(ev.Slots), Revision: ev.Revision, Equipment: equipSlots(ev.Equipment),
		}}
	case world.Loot:
		m.Event = &worldv1.DeliverResponse_Loot{Loot: &worldv1.Loot{
			Kind: lootKind(ev.Kind), MapId: ev.MapID, DropId: ev.DropID, OwnerId: ev.Owner.String(), OwnerName: ev.OwnerName, Template: ev.Template, Quantity: int32(ev.Quantity),
		}}
	case world.Detached:
		m.Event = &worldv1.DeliverResponse_Detached{Detached: &worldv1.Detached{}}
	case world.NpcDialog:
		m.Event = &worldv1.DeliverResponse_NpcDialog{NpcDialog: npcDialog(ev)}
	case world.QuestChanged:
		m.Event = &worldv1.DeliverResponse_QuestChanged{QuestChanged: &worldv1.QuestChanged{Change: questChanges[ev.Change], Quest: questState(ev.ID, ev.Quest)}}
	}
	if m.Event == nil {
		return nil // no event: nothing to send
	}
	return m
}

func fieldBoss(b world.FieldBoss) *worldv1.FieldBoss {
	return &worldv1.FieldBoss{MobId: b.Mob, Type: b.Type, LeavesInMs: b.LeavesIn.Milliseconds()}
}

var questChanges = map[world.QuestChange]worldv1.QuestChange{
	world.QuestProgressed: worldv1.QuestChange_QUEST_CHANGE_PROGRESSED,
	world.QuestAccepted:   worldv1.QuestChange_QUEST_CHANGE_ACCEPTED,
	world.QuestHandedIn:   worldv1.QuestChange_QUEST_CHANGE_HANDED_IN,
	world.QuestAbandoned:  worldv1.QuestChange_QUEST_CHANGE_ABANDONED,
}

var questStatuses = map[world.QuestStatus]worldv1.QuestStatus{
	world.QuestActive:   worldv1.QuestStatus_QUEST_STATUS_ACTIVE,
	world.QuestFinished: worldv1.QuestStatus_QUEST_STATUS_FINISHED,
}

// questState is one quest of a log; a zero quest, one that left the log, has no status.
func questState(id string, q world.Quest) *worldv1.QuestState {
	progress := make([]int32, len(q.Progress))
	for k, n := range q.Progress {
		progress[k] = int32(n) //nolint:gosec // counts are small
	}
	return &worldv1.QuestState{QuestId: id, Status: questStatuses[q.Status], Progress: progress, TimesFinished: int32(q.TimesFinished)} //nolint:gosec // counts are small
}

var offerStates = map[world.OfferState]worldv1.QuestOfferState{
	world.OfferAvailable: worldv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE,
	world.OfferActive:    worldv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE,
	world.OfferFinished:  worldv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED,
	world.OfferUnmet:     worldv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET,
}

// leveledView is the player's character as a level-up or a promotion leaves it.
func leveledView(ev world.LevelUp) *worldv1.CharacterView {
	return &worldv1.CharacterView{
		Id: ev.ID.String(), Name: ev.Name, Class: ev.Class.String(), Level: int32(ev.Level), Exp: ev.Exp, Stats: stats(ev.Stats), //nolint:gosec // levels are small
		Hp: int32(ev.HP), MaxHp: int32(ev.MaxHP), Sheet: statSheet(ev.Sheet),
	}
}

var conditionKinds = map[world.ConditionKind]worldv1.PromotionConditionKind{
	world.ConditionLevel: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL,
	world.ConditionStat:  worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT,
	world.ConditionSkill: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL,
	world.ConditionQuest: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST,
}

func npcDialog(d world.NpcDialog) *worldv1.NpcDialog {
	out := &worldv1.NpcDialog{NpcId: d.NPC}
	for _, o := range d.Gives {
		offer := &worldv1.QuestOffer{QuestId: o.Quest, State: offerStates[o.State]}
		if o.State == world.OfferUnmet {
			offer.Unmet = &worldv1.QuestConditions{Level: int32(o.Unmet.Level), Class: o.Unmet.Class.String(), Quests: o.Unmet.Quests} //nolint:gosec // levels are small
		}
		out.Gives = append(out.Gives, offer)
	}
	for _, t := range d.Takes {
		out.Takes = append(out.Takes, &worldv1.QuestTurnIn{QuestId: t.Quest, Ready: t.Ready})
	}
	if p := d.Promotion; p != nil {
		out.Promotion = &worldv1.PromotionOffer{Class: p.Class.String()}
		for _, c := range p.Conditions {
			out.Promotion.Conditions = append(out.Promotion.Conditions, &worldv1.PromotionCondition{Kind: conditionKinds[c.Kind], Id: c.ID, Need: int32(c.Need), Have: int32(c.Have)}) //nolint:gosec // conditions are small
		}
	}
	return out
}

func lootKind(k world.LootKind) worldv1.LootKind {
	switch k {
	case world.LootCollected:
		return worldv1.LootKind_LOOT_KIND_COLLECTED
	case world.LootWaiting:
		return worldv1.LootKind_LOOT_KIND_WAITING
	case world.LootLost:
		return worldv1.LootKind_LOOT_KIND_LOST
	}
	return worldv1.LootKind_LOOT_KIND_UNSPECIFIED
}

func entityRef(r world.EntityRef) *worldv1.EntityRef {
	if r.IsMob() {
		return &worldv1.EntityRef{Ref: &worldv1.EntityRef_MobId{MobId: r.Mob}}
	}
	return &worldv1.EntityRef{Ref: &worldv1.EntityRef_CharacterId{CharacterId: r.Player.String()}}
}

func stats(s gamedata.Stats) *worldv1.Stats {
	return &worldv1.Stats{Strength: int32(s.STR), Dexterity: int32(s.DEX), Intelligence: int32(s.INT), Agility: int32(s.AGI), Vitality: int32(s.VIT)}
}

func playerViews(ps []world.PlayerView) []*worldv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*worldv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &worldv1.PlayerView{
			Id: p.ID.String(), Name: p.Name, Class: p.Class.String(), Level: int32(p.Level), Hp: int32(p.HP), MaxHp: int32(p.MaxHP),
			Dead: p.Dead, Invulnerable: p.Invulnerable,
			X: p.State.X, Y: p.State.Y, Vx: p.State.VX, Vy: p.State.VY, Facing: int32(p.State.Facing),
			OnGround: p.State.OnGround, Climbing: p.State.Climbing, Casting: uint32(p.State.Casting), //nolint:gosec // never negative
		})
	}
	return out
}

func mobViews(ms []world.MobView) []*worldv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*worldv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, &worldv1.MobView{Id: m.ID, Type: m.Type, X: m.X, Y: m.Y, Facing: int32(m.Facing), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), Pose: mobPoses[m.Pose]})
	}
	return out
}

var mobPoses = map[world.MobPose]worldv1.MobPose{
	world.Standing:  worldv1.MobPose_MOB_POSE_UNSPECIFIED,
	world.Flinching: worldv1.MobPose_MOB_POSE_FLINCH,
	world.Down:      worldv1.MobPose_MOB_POSE_DOWN,
	world.Rising:    worldv1.MobPose_MOB_POSE_RISING,
}

func dropViews(ds []world.DropView) []*worldv1.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]*worldv1.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, &worldv1.DropView{Id: d.ID, Template: d.Template, Quantity: int32(d.Quantity), X: d.X, Y: d.Y, OwnerId: d.Owner.String()})
	}
	return out
}

func slots(in []world.Slot) []*worldv1.Slot {
	out := make([]*worldv1.Slot, 0, len(in))
	for _, s := range in {
		slot := &worldv1.Slot{Tab: string(s.Tab), Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = item(s.Item)
			slot.Item.Tab, slot.Item.Slot = string(s.Tab), int32(s.Slot)
		}
		out = append(out, slot)
	}
	return out
}

func equipSlots(in []world.EquipSlot) []*worldv1.EquipSlot {
	if len(in) == 0 {
		return nil
	}
	out := make([]*worldv1.EquipSlot, 0, len(in))
	for _, s := range in {
		slot := &worldv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = item(s.Item)
		}
		out = append(out, slot)
	}
	return out
}

func item(it *world.SlotItem) *worldv1.Item {
	out := &worldv1.Item{Id: it.ID.String(), TemplateId: it.Template, Quantity: int32(it.Quantity)}
	if s := it.Stats; s != nil {
		//nolint:gosec // stats are small
		out.Stats = &worldv1.ItemStats{
			Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
			PhysicalAttack: int32(s.PhysicalAttack), MagicAttack: int32(s.MagicAttack), PhysicalDefense: int32(s.PhysicalDefense), MagicDefense: int32(s.MagicDefense),
			MaxHp: int32(s.MaxHP), Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion),
			CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), Penetration: int32(s.Penetration), HpRegen: int32(s.HPRegen),
			CooldownSpeed: int32(s.CooldownSpeed), MoveSpeed: int32(s.MoveSpeed),
		}
	}
	return out
}

func statSheet(s world.Sheet) *worldv1.StatSheet {
	return &worldv1.StatSheet{
		Primary: stats(s.Primary), PrimaryWorn: stats(s.PrimaryWorn), Derived: derived(s.Derived), DerivedWorn: derived(s.DerivedWorn),
		PrimarySkills: stats(s.PrimarySkills), DerivedSkills: derived(s.DerivedSkills), Skills: skillPoints(s.Skills),
		Points:            &worldv1.Points{Unspent: int32(s.Points.Unspent), Spent: stats(s.Points.Spent), FreeResetLeft: !s.Points.FreeResetUsed}, //nolint:gosec // points are small
		AttackLengthTicks: uint32(s.AttackLengthTicks),                                                                                             //nolint:gosec // an attack is a few ticks
	}
}

func skillPoints(s world.Skills) *worldv1.SkillPoints {
	out := &worldv1.SkillPoints{Unspent: int32(s.Unspent), Invested: make(map[string]int32, len(s.Points))} //nolint:gosec // points are small
	for skill, n := range s.Points {
		out.Invested[skill] = int32(n) //nolint:gosec // points are small
	}
	return out
}

func derived(d progression.Derived) *worldv1.DerivedStats {
	return &worldv1.DerivedStats{
		PhysicalAttack: d.PhysicalAttack, MagicAttack: d.MagicAttack, PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense,
		Accuracy: d.Accuracy, Evasion: d.Evasion, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration,
		HpRegen: d.HPRegen, CooldownSpeed: d.CooldownSpeed, MoveSpeed: d.MoveSpeed,
		MaxHp: int32(d.MaxHP), //nolint:gosec // HP is small
	}
}

// encodeFrame is the frame as a Delta without an AckSeq; each recipient has its own.
func encodeFrame(f world.Frame) *worldv1.Delta {
	d := &worldv1.Delta{
		Players: playerViews(f.Players), Mobs: mobViews(f.Mobs), Drops: dropViews(f.Drops),
		RemovedMobs: f.RemovedMobs, RemovedDrops: f.RemovedDrops, CollectedDrops: f.CollectedDrops,
	}
	for _, id := range f.Removed {
		d.RemovedPlayers = append(d.RemovedPlayers, id.String())
	}
	return d
}
