package local

import (
	"finga/internal/progression"
	"finga/services/gateway/internal/model"
	"finga/services/world"
)

// delivered turns one world delivery into its events: a Frame into one per recipient, all sharing it.
func delivered(d world.Delivery, deliver func(model.WorldEvent)) {
	switch ev := d.Event.(type) {
	case world.Frame:
		frame := frameOf(ev)
		for _, r := range ev.Recipients {
			var energy *uint32
			if r.Energy != nil {
				e := uint32(*r.Energy) //nolint:gosec // energy is 0 to the data's max
				energy = &e
			}
			deliver(model.WorldDelivery{Connection: r.Connection, Character: r.Character, Delivery: model.Delivery{Tick: d.Tick, Push: model.Delta{Frame: frame, AckSeq: r.AckSeq, Energy: energy}}})
		}
	case world.Detached:
		deliver(model.Detached{Connection: d.Conn, Character: d.Character})
	default:
		if p, ok := worldPush(d.Event); ok {
			deliver(model.WorldDelivery{Connection: d.Conn, Character: d.Character, Delivery: model.Delivery{Tick: d.Tick, Push: p}})
		}
	}
}

// worldPush turns a player's event into its push; false for one with no push.
func worldPush(e world.Event) (model.Push, bool) {
	switch ev := e.(type) {
	case world.Snapshot:
		snap := model.Snapshot{Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Drops: dropViews(ev.Drops)}
		if ev.FieldBoss != nil {
			boss := fieldBoss(*ev.FieldBoss)
			snap.FieldBoss = &boss
		}
		return snap, true
	case world.FieldBoss:
		return fieldBoss(ev), true
	case world.FieldBossGone:
		return model.FieldBossGone{MobID: ev.Mob, Beaten: ev.Beaten}, true
	case world.Teleport:
		return model.MapChange{MapID: ev.MapID, X: ev.X, Y: ev.Y}, true
	case world.Damage:
		return model.Damage{Source: entity(ev.Source), Target: entity(ev.Target), Amount: ev.Amount, Crit: ev.Crit, Miss: ev.Miss}, true
	case world.SkillUsed:
		return model.SkillUsed{Caster: ev.Caster.String(), Skill: ev.Skill, Stage: ev.Stage, Facing: ev.Facing}, true
	case world.AttackUsed:
		return model.AttackUsed{Attacker: ev.Attacker.String(), Facing: ev.Facing, LengthTicks: int(ev.Length)}, true //nolint:gosec // lengths are small
	case world.MobAttacked:
		return model.MobAttacked{MobID: ev.Mob, Attack: ev.Attack, Facing: ev.Facing, To: ev.To}, true
	case world.Died:
		return model.Died{CharacterID: ev.ID.String(), X: ev.X, Y: ev.Y}, true
	case world.Respawned:
		return model.Respawned{CharacterID: ev.ID.String(), MapID: ev.MapID, X: ev.X, Y: ev.Y, HP: ev.HP}, true
	case world.ExpGained:
		return model.ExpGained{Amount: ev.Amount, Exp: ev.Exp}, true
	case world.LevelUp:
		return model.LevelUp{Character: leveledView(ev)}, true
	case world.Promoted:
		return model.Promoted{Character: leveledView(world.LevelUp(ev))}, true
	case world.Chat:
		return model.ChatMessage{Channel: model.ChatMap, Sender: ev.Sender.String(), SenderName: ev.SenderName, Text: ev.Text}, true
	case world.InventoryChanged:
		return model.InventoryChanged{Revision: ev.Revision, Slots: slots(ev.Slots), Equipment: equipSlots(ev.Equipment)}, true
	case world.StatsChanged:
		sc := model.StatsChanged{HP: ev.HP, MaxHP: ev.MaxHP}
		if ev.Sheet != nil {
			sheet := statSheet(*ev.Sheet)
			sc.Sheet = &sheet
		}
		return sc, true
	case world.Loot:
		return model.Loot{
			Kind: lootKinds[ev.Kind], MapID: ev.MapID, DropID: ev.DropID, Owner: ev.Owner.String(), OwnerName: ev.OwnerName,
			Template: ev.Template, Quantity: ev.Quantity,
		}, true
	case world.NpcDialog:
		return npcDialog(ev), true
	case world.QuestChanged:
		return model.QuestChanged{Change: questChanges[ev.Change], Quest: questState(ev.ID, ev.Quest)}, true
	case world.Frame, world.Detached:
	}
	return nil, false
}

func fieldBoss(b world.FieldBoss) model.FieldBoss {
	return model.FieldBoss{MobID: b.Mob, Type: b.Type, LeavesInMs: b.LeavesIn.Milliseconds()}
}

// lootKinds, questChanges, questStatuses, offerStates, conditionKinds and mobPoses are the world's enums as the gateway's; an unknown value is the zero one.
var (
	lootKinds = map[world.LootKind]model.LootKind{
		world.LootCollected: model.LootCollected,
		world.LootWaiting:   model.LootWaiting,
		world.LootLost:      model.LootLost,
	}
	questChanges = map[world.QuestChange]model.QuestChange{
		world.QuestProgressed: model.QuestProgressed,
		world.QuestAccepted:   model.QuestAccepted,
		world.QuestHandedIn:   model.QuestHandedIn,
		world.QuestAbandoned:  model.QuestAbandoned,
	}
	questStatuses = map[world.QuestStatus]model.QuestStatus{
		world.QuestActive:   model.QuestActive,
		world.QuestFinished: model.QuestFinished,
	}
	offerStates = map[world.OfferState]model.QuestOfferState{
		world.OfferAvailable: model.OfferAvailable,
		world.OfferActive:    model.OfferActive,
		world.OfferFinished:  model.OfferFinished,
		world.OfferUnmet:     model.OfferUnmet,
	}
	conditionKinds = map[world.ConditionKind]model.ConditionKind{
		world.ConditionLevel: model.ConditionLevel,
		world.ConditionStat:  model.ConditionStat,
		world.ConditionSkill: model.ConditionSkill,
		world.ConditionQuest: model.ConditionQuest,
	}
	mobPoses = map[world.MobPose]model.MobPose{
		world.Standing:  model.Standing,
		world.Flinching: model.Flinching,
		world.Down:      model.Down,
		world.Rising:    model.Rising,
	}
)

// questState is one quest of a log, its counts the facade's own copy; a zero quest, one that left the log, has no status.
func questState(id string, q world.Quest) model.QuestState {
	return model.QuestState{Quest: id, Status: questStatuses[q.Status], Progress: q.Progress, TimesFinished: q.TimesFinished}
}

func npcDialog(d world.NpcDialog) model.NpcDialog {
	out := model.NpcDialog{NPC: d.NPC}
	for _, o := range d.Gives {
		offer := model.QuestOffer{Quest: o.Quest, State: offerStates[o.State]}
		if o.State == world.OfferUnmet {
			offer.Unmet = model.QuestConditions{Level: o.Unmet.Level, Class: o.Unmet.Class.String(), Quests: o.Unmet.Quests}
		}
		out.Gives = append(out.Gives, offer)
	}
	for _, t := range d.Takes {
		out.Takes = append(out.Takes, model.QuestTurnIn{Quest: t.Quest, Ready: t.Ready})
	}
	if p := d.Promotion; p != nil {
		out.Promotion = &model.PromotionOffer{Class: p.Class.String()}
		for _, c := range p.Conditions {
			out.Promotion.Conditions = append(out.Promotion.Conditions, model.PromotionCondition{Kind: conditionKinds[c.Kind], ID: c.ID, Need: c.Need, Have: c.Have})
		}
	}
	return out
}

func entity(r world.EntityRef) model.Entity {
	if r.IsMob() {
		return model.Entity{Mob: r.Mob}
	}
	return model.Entity{Character: r.Player.String()}
}

// leveledView is the player's character as a level-up or a promotion leaves it; its energy comes with the frames.
func leveledView(ev world.LevelUp) model.CharacterView {
	return model.CharacterView{
		ID: ev.ID.String(), Name: ev.Name, Class: ev.Class.String(), Level: ev.Level, Exp: ev.Exp, Stats: stats(ev.Stats),
		HP: ev.HP, MaxHP: ev.MaxHP, Sheet: statSheet(ev.Sheet),
	}
}

func statSheet(s world.Sheet) model.StatSheet {
	return model.StatSheet{
		Primary: stats(s.Primary), PrimaryWorn: stats(s.PrimaryWorn), Derived: derived(s.Derived), DerivedWorn: derived(s.DerivedWorn),
		PrimarySkills: stats(s.PrimarySkills), DerivedSkills: derived(s.DerivedSkills), Skills: model.SkillPoints{Unspent: s.Skills.Unspent, Invested: s.Skills.Points},
		Points:            model.Points{Unspent: s.Points.Unspent, Spent: stats(s.Points.Spent), FreeResetLeft: !s.Points.FreeResetUsed},
		AttackLengthTicks: int(s.AttackLengthTicks), //nolint:gosec // an attack is a few ticks
	}
}

func derived(d progression.Derived) model.DerivedStats {
	return model.DerivedStats{
		PhysicalAttack: d.PhysicalAttack, MagicAttack: d.MagicAttack, PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense,
		Accuracy: d.Accuracy, Evasion: d.Evasion, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration,
		HPRegen: d.HPRegen, CooldownSpeed: d.CooldownSpeed, MoveSpeed: d.MoveSpeed,
		MaxHP: d.MaxHP,
	}
}

// frameOf is a Frame as every recipient shares it, read-only as the facade's is.
func frameOf(f world.Frame) *model.Frame {
	out := &model.Frame{
		Players: playerViews(f.Players), Mobs: mobViews(f.Mobs), Drops: dropViews(f.Drops),
		RemovedMobs: f.RemovedMobs, RemovedDrops: f.RemovedDrops, CollectedDrops: f.CollectedDrops,
	}
	for _, id := range f.Removed {
		out.RemovedPlayers = append(out.RemovedPlayers, id.String())
	}
	return out
}

func playerViews(ps []world.PlayerView) []model.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]model.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, model.PlayerView{
			ID: p.ID.String(), Name: p.Name, Class: p.Class.String(), Level: p.Level,
			X: p.State.X, Y: p.State.Y, VX: p.State.VX, VY: p.State.VY, Facing: p.State.Facing,
			OnGround: p.State.OnGround, Climbing: p.State.Climbing, HP: p.HP, MaxHP: p.MaxHP, Dead: p.Dead,
			Invulnerable: p.Invulnerable, Casting: p.State.Casting,
		})
	}
	return out
}

func mobViews(ms []world.MobView) []model.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]model.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, model.MobView{ID: m.ID, Type: m.Type, X: m.X, Y: m.Y, Facing: m.Facing, HP: m.HP, MaxHP: m.MaxHP, Pose: mobPoses[m.Pose]})
	}
	return out
}

func dropViews(ds []world.DropView) []model.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]model.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, model.DropView{ID: d.ID, Template: d.Template, Quantity: d.Quantity, X: d.X, Y: d.Y, Owner: d.Owner.String()})
	}
	return out
}

// slots are the slots a change touched, each item in its slot.
func slots(in []world.Slot) []model.InventorySlot {
	out := make([]model.InventorySlot, 0, len(in))
	for _, s := range in {
		slot := model.InventorySlot{Tab: string(s.Tab), Slot: s.Slot}
		if s.Item != nil {
			slot.Item = slotItem(s.Item, string(s.Tab), s.Slot)
		}
		out = append(out, slot)
	}
	return out
}

// equipSlots are the equip slots a change touched; a worn item is in no tab.
func equipSlots(in []world.EquipSlot) []model.EquipSlot {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.EquipSlot, 0, len(in))
	for _, s := range in {
		slot := model.EquipSlot{Slot: s.Slot}
		if s.Item != nil {
			slot.Item = slotItem(s.Item, "", 0)
		}
		out = append(out, slot)
	}
	return out
}

func slotItem(it *world.SlotItem, tab string, slot int) *model.ItemView {
	return &model.ItemView{ID: it.ID.String(), Template: it.Template, Tab: tab, Slot: slot, Quantity: it.Quantity, Stats: rollOf(it.Stats)}
}
