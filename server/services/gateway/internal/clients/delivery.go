package clients

import (
	worldv1 "finga/api/gen/world/v1"
	"finga/services/gateway/internal/model"
)

// worldPush turns a world stream event into its push; false for an unknown event.
func worldPush(m *worldv1.DeliverResponse) (model.Push, bool) {
	switch {
	case m.GetSnapshot() != nil:
		ev := m.GetSnapshot()
		snap := model.Snapshot{Players: playerViews(ev.GetPlayers()), Mobs: mobViews(ev.GetMobs()), Drops: dropViews(ev.GetDrops())}
		if b := ev.GetFieldBoss(); b != nil {
			boss := fieldBoss(b)
			snap.FieldBoss = &boss
		}
		return snap, true
	case m.GetFieldBoss() != nil:
		return fieldBoss(m.GetFieldBoss()), true
	case m.GetFieldBossGone() != nil:
		ev := m.GetFieldBossGone()
		return model.FieldBossGone{MobID: ev.GetMobId(), Beaten: ev.GetBeaten()}, true
	case m.GetTeleport() != nil:
		ev := m.GetTeleport()
		return model.MapChange{MapID: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY()}, true
	case m.GetDamage() != nil:
		ev := m.GetDamage()
		return model.Damage{Source: entity(ev.GetSource()), Target: entity(ev.GetTarget()), Amount: int(ev.GetAmount()), Crit: ev.GetCrit(), Miss: ev.GetMiss()}, true
	case m.GetSkillUsed() != nil:
		ev := m.GetSkillUsed()
		return model.SkillUsed{Caster: ev.GetCaster(), Skill: ev.GetSkill(), Stage: int(ev.GetStage()), Facing: int(ev.GetFacing())}, true
	case m.GetAttackUsed() != nil:
		ev := m.GetAttackUsed()
		return model.AttackUsed{Attacker: ev.GetAttacker(), Facing: int(ev.GetFacing()), LengthTicks: int(ev.GetLengthTicks())}, true
	case m.GetMobAttacked() != nil:
		ev := m.GetMobAttacked()
		return model.MobAttacked{MobID: ev.GetMob(), Attack: int(ev.GetAttack()), Facing: int(ev.GetFacing())}, true
	case m.GetDied() != nil:
		ev := m.GetDied()
		return model.Died{CharacterID: ev.GetCharacterId(), X: ev.GetX(), Y: ev.GetY()}, true
	case m.GetRespawned() != nil:
		ev := m.GetRespawned()
		return model.Respawned{CharacterID: ev.GetCharacterId(), MapID: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY(), HP: int(ev.GetHp())}, true
	case m.GetExpGained() != nil:
		ev := m.GetExpGained()
		return model.ExpGained{Amount: ev.GetAmount(), Exp: ev.GetExp()}, true
	case m.GetLevelUp() != nil:
		return model.LevelUp{Character: characterView(m.GetLevelUp().GetCharacter())}, true
	case m.GetPromoted() != nil:
		return model.Promoted{Character: characterView(m.GetPromoted().GetCharacter())}, true
	case m.GetChat() != nil:
		ev := m.GetChat()
		return model.ChatMessage{Channel: model.ChatMap, Sender: ev.GetSenderId(), SenderName: ev.GetSenderName(), Text: ev.GetText()}, true
	case m.GetInventoryChanged() != nil:
		ev := m.GetInventoryChanged()
		return model.InventoryChanged{Revision: ev.GetRevision(), Slots: worldSlots(ev.GetSlots()), Equipment: worldEquipment(ev.GetEquipment())}, true
	case m.GetStatsChanged() != nil:
		ev := m.GetStatsChanged()
		sc := model.StatsChanged{HP: int(ev.GetHp()), MaxHP: int(ev.GetMaxHp())}
		if ev.GetSheet() != nil { // statSheet would read an unset sheet as a zeroed one
			sheet := statSheet(ev.GetSheet())
			sc.Sheet = &sheet
		}
		return sc, true
	case m.GetLoot() != nil:
		ev := m.GetLoot()
		return model.Loot{
			Kind: lootKinds[ev.GetKind()], MapID: ev.GetMapId(), DropID: ev.GetDropId(), Owner: ev.GetOwnerId(), OwnerName: ev.GetOwnerName(),
			Template: ev.GetTemplate(), Quantity: int(ev.GetQuantity()),
		}, true
	case m.GetNpcDialog() != nil:
		return npcDialog(m.GetNpcDialog()), true
	case m.GetQuestChanged() != nil:
		ev := m.GetQuestChanged()
		return model.QuestChanged{Change: questChanges[ev.GetChange()], Quest: questState(ev.GetQuest())}, true
	}
	return nil, false
}

func fieldBoss(b *worldv1.FieldBoss) model.FieldBoss {
	return model.FieldBoss{MobID: b.GetMobId(), Type: b.GetType(), LeavesInMs: b.GetLeavesInMs()}
}

func npcDialog(d *worldv1.NpcDialog) model.NpcDialog {
	out := model.NpcDialog{NPC: d.GetNpcId()}
	for _, o := range d.GetGives() {
		u := o.GetUnmet()
		out.Gives = append(out.Gives, model.QuestOffer{
			Quest: o.GetQuestId(), State: offerStates[o.GetState()],
			Unmet: model.QuestConditions{Level: int(u.GetLevel()), Class: u.GetClass(), Quests: u.GetQuests()},
		})
	}
	for _, t := range d.GetTakes() {
		out.Takes = append(out.Takes, model.QuestTurnIn{Quest: t.GetQuestId(), Ready: t.GetReady()})
	}
	if p := d.GetPromotion(); p != nil {
		out.Promotion = &model.PromotionOffer{Class: p.GetClass()}
		for _, c := range p.GetConditions() {
			out.Promotion.Conditions = append(out.Promotion.Conditions, model.PromotionCondition{Kind: conditionKinds[c.GetKind()], ID: c.GetId(), Need: int(c.GetNeed()), Have: int(c.GetHave())})
		}
	}
	return out
}

var conditionKinds = map[worldv1.PromotionConditionKind]model.ConditionKind{
	worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL: model.ConditionLevel,
	worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT:  model.ConditionStat,
	worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL: model.ConditionSkill,
	worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST: model.ConditionQuest,
}

func questStates(qs []*worldv1.QuestState) []model.QuestState {
	if len(qs) == 0 {
		return nil
	}
	out := make([]model.QuestState, 0, len(qs))
	for _, q := range qs {
		out = append(out, questState(q))
	}
	return out
}

func questState(q *worldv1.QuestState) model.QuestState {
	var progress []int
	for _, n := range q.GetProgress() {
		progress = append(progress, int(n))
	}
	return model.QuestState{Quest: q.GetQuestId(), Status: questStatuses[q.GetStatus()], Progress: progress, TimesFinished: int(q.GetTimesFinished())}
}

// offerStates, questStatuses and questChanges are the world's quest enums; an unknown value is the zero one.
var (
	offerStates = map[worldv1.QuestOfferState]model.QuestOfferState{
		worldv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE:        model.OfferAvailable,
		worldv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE:           model.OfferActive,
		worldv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED:         model.OfferFinished,
		worldv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET: model.OfferUnmet,
	}
	questStatuses = map[worldv1.QuestStatus]model.QuestStatus{
		worldv1.QuestStatus_QUEST_STATUS_ACTIVE:   model.QuestActive,
		worldv1.QuestStatus_QUEST_STATUS_FINISHED: model.QuestFinished,
	}
	questChanges = map[worldv1.QuestChange]model.QuestChange{
		worldv1.QuestChange_QUEST_CHANGE_PROGRESSED: model.QuestProgressed,
		worldv1.QuestChange_QUEST_CHANGE_ACCEPTED:   model.QuestAccepted,
		worldv1.QuestChange_QUEST_CHANGE_HANDED_IN:  model.QuestHandedIn,
		worldv1.QuestChange_QUEST_CHANGE_ABANDONED:  model.QuestAbandoned,
	}
)

// frameOf is a Frame's Delta, which every recipient shares.
func frameOf(d *worldv1.Delta) *model.Frame {
	return &model.Frame{
		Players: playerViews(d.GetPlayers()), Mobs: mobViews(d.GetMobs()), Drops: dropViews(d.GetDrops()),
		RemovedPlayers: d.GetRemovedPlayers(), RemovedMobs: d.GetRemovedMobs(), RemovedDrops: d.GetRemovedDrops(), CollectedDrops: d.GetCollectedDrops(),
	}
}

// lootKinds are the world's kinds of loot; an unknown one is the zero kind.
var lootKinds = map[worldv1.LootKind]model.LootKind{
	worldv1.LootKind_LOOT_KIND_COLLECTED: model.LootCollected,
	worldv1.LootKind_LOOT_KIND_WAITING:   model.LootWaiting,
	worldv1.LootKind_LOOT_KIND_LOST:      model.LootLost,
}

func entity(r *worldv1.EntityRef) model.Entity {
	return model.Entity{Character: r.GetCharacterId(), Mob: r.GetMobId()}
}

func characterView(c *worldv1.CharacterView) model.CharacterView {
	return model.CharacterView{
		ID: c.GetId(), Name: c.GetName(), Class: c.GetClass(), Level: int(c.GetLevel()), Exp: c.GetExp(), Stats: stats(c.GetStats()),
		HP: int(c.GetHp()), MaxHP: int(c.GetMaxHp()), Energy: int(c.GetEnergy()), Sheet: statSheet(c.GetSheet()),
	}
}

func stats(st *worldv1.Stats) model.Stats {
	return model.Stats{STR: int(st.GetStrength()), DEX: int(st.GetDexterity()), INT: int(st.GetIntelligence()), AGI: int(st.GetAgility()), VIT: int(st.GetVitality())}
}

func statSheet(s *worldv1.StatSheet) model.StatSheet {
	p := s.GetPoints()
	return model.StatSheet{
		Primary: stats(s.GetPrimary()), PrimaryWorn: stats(s.GetPrimaryWorn()), Derived: derived(s.GetDerived()), DerivedWorn: derived(s.GetDerivedWorn()),
		PrimarySkills: stats(s.GetPrimarySkills()), DerivedSkills: derived(s.GetDerivedSkills()), Skills: skillPoints(s.GetSkills()),
		Points:            model.Points{Unspent: int(p.GetUnspent()), Spent: stats(p.GetSpent()), FreeResetLeft: p.GetFreeResetLeft()},
		AttackLengthTicks: int(s.GetAttackLengthTicks()),
	}
}

func skillPoints(s *worldv1.SkillPoints) model.SkillPoints {
	out := model.SkillPoints{Unspent: int(s.GetUnspent()), Invested: make(map[string]int, len(s.GetInvested()))}
	for skill, n := range s.GetInvested() {
		out.Invested[skill] = int(n)
	}
	return out
}

func derived(d *worldv1.DerivedStats) model.DerivedStats {
	return model.DerivedStats{
		PhysicalAttack: d.GetPhysicalAttack(), MagicAttack: d.GetMagicAttack(), PhysicalDefense: d.GetPhysicalDefense(), MagicDefense: d.GetMagicDefense(),
		Accuracy: d.GetAccuracy(), Evasion: d.GetEvasion(), CritRate: d.GetCritRate(), CritDamage: d.GetCritDamage(), Penetration: d.GetPenetration(),
		HPRegen: d.GetHpRegen(), CooldownSpeed: d.GetCooldownSpeed(), MoveSpeed: d.GetMoveSpeed(),
		MaxHP: int(d.GetMaxHp()),
	}
}

func playerViews(ps []*worldv1.PlayerView) []model.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]model.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, model.PlayerView{
			ID: p.GetId(), Name: p.GetName(), Class: p.GetClass(), Level: int(p.GetLevel()),
			X: p.GetX(), Y: p.GetY(), VX: p.GetVx(), VY: p.GetVy(), Facing: int(p.GetFacing()),
			OnGround: p.GetOnGround(), Climbing: p.GetClimbing(), HP: int(p.GetHp()), MaxHP: int(p.GetMaxHp()), Dead: p.GetDead(),
			Invulnerable: p.GetInvulnerable(), Casting: int(p.GetCasting()),
		})
	}
	return out
}

func mobViews(ms []*worldv1.MobView) []model.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]model.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, model.MobView{ID: m.GetId(), Type: m.GetType(), X: m.GetX(), Y: m.GetY(), Facing: int(m.GetFacing()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp()), Pose: mobPoses[m.GetPose()]})
	}
	return out
}

var mobPoses = map[worldv1.MobPose]model.MobPose{
	worldv1.MobPose_MOB_POSE_FLINCH: model.Flinching,
	worldv1.MobPose_MOB_POSE_DOWN:   model.Down,
	worldv1.MobPose_MOB_POSE_RISING: model.Rising,
}

func dropViews(ds []*worldv1.DropView) []model.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]model.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, model.DropView{ID: d.GetId(), Template: d.GetTemplate(), Quantity: int(d.GetQuantity()), X: d.GetX(), Y: d.GetY(), Owner: d.GetOwnerId()})
	}
	return out
}

// worldSlots are the slots a grant or an equip changed, as the world's contract carries them.
func worldSlots(slots []*worldv1.Slot) []model.InventorySlot {
	out := make([]model.InventorySlot, 0, len(slots))
	for _, s := range slots {
		out = append(out, model.InventorySlot{Tab: s.GetTab(), Slot: int(s.GetSlot()), Item: worldItem(s.GetItem())})
	}
	return out
}

// roll is an item's roll in the item service's contract or the world's, which share its fields.
type roll interface {
	GetPhysicalAttack() int32
	GetMagicAttack() int32
	GetPhysicalDefense() int32
	GetMagicDefense() int32
	GetPenetration() int32
	GetHpRegen() int32
	GetCooldownSpeed() int32
	GetMoveSpeed() int32
	GetStr() int32
	GetDex() int32
	GetInt() int32
	GetAgi() int32
	GetVit() int32
	GetAccuracy() int32
	GetEvasion() int32
	GetCritRate() int32
	GetCritDamage() int32
	GetMaxHp() int32
}

func rollOf(s roll) *model.ItemStats {
	return &model.ItemStats{
		STR: int(s.GetStr()), DEX: int(s.GetDex()), INT: int(s.GetInt()), AGI: int(s.GetAgi()), VIT: int(s.GetVit()),
		PhysicalAttack: int(s.GetPhysicalAttack()), MagicAttack: int(s.GetMagicAttack()), PhysicalDefense: int(s.GetPhysicalDefense()), MagicDefense: int(s.GetMagicDefense()),
		MaxHP: int(s.GetMaxHp()), Accuracy: int(s.GetAccuracy()), Evasion: int(s.GetEvasion()),
		CritRate: int(s.GetCritRate()), CritDamage: int(s.GetCritDamage()), Penetration: int(s.GetPenetration()), HPRegen: int(s.GetHpRegen()),
		CooldownSpeed: int(s.GetCooldownSpeed()), MoveSpeed: int(s.GetMoveSpeed()),
	}
}

// worldEquipment are the equip slots an equip or unequip changed.
func worldEquipment(slots []*worldv1.EquipSlot) []model.EquipSlot {
	if len(slots) == 0 {
		return nil
	}
	out := make([]model.EquipSlot, 0, len(slots))
	for _, s := range slots {
		out = append(out, model.EquipSlot{Slot: s.GetEquipSlot(), Item: worldItem(s.GetItem())})
	}
	return out
}

func worldItem(it *worldv1.Item) *model.ItemView {
	if it == nil {
		return nil
	}
	view := &model.ItemView{ID: it.GetId(), Template: it.GetTemplateId(), Tab: it.GetTab(), Slot: int(it.GetSlot()), Quantity: int(it.GetQuantity())}
	if s := it.GetStats(); s != nil {
		view.Stats = rollOf(s)
	}
	return view
}
