package gateway

import (
	"time"

	"finga/internal/application/world"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
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
			delta := model.Delta{Frame: frame, Parts: r.Parts, AckSeq: r.AckSeq, Energy: energy, Unseen: unseenOf(r.Unseen), Pouches: pouchViews(r.Pouches.Changed), PouchesGone: r.Pouches.Gone}
			deliver(model.WorldDelivery{Connection: r.Connection, Character: r.Character, Delivery: model.Delivery{Tick: d.Tick, Push: delta}})
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
		snap := model.Snapshot{Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Pouches: pouchViews(ev.Pouches), Boss: mobView(ev.Boss)}
		if ev.FieldBoss != nil {
			boss := fieldBoss(*ev.FieldBoss)
			snap.FieldBoss = &boss
		}
		if ev.FieldBossGone != nil {
			gone := bossGone(*ev.FieldBossGone)
			snap.FieldBossGone = &gone
		}
		return snap, true
	case world.FieldBoss:
		return fieldBoss(ev), true
	case world.FieldBossGone:
		return bossGone(ev), true
	case world.Teleport:
		return model.MapChange{MapID: ev.MapID, X: ev.X, Y: ev.Y, Z: ev.Z, Yaw: ev.Yaw}, true
	case world.Damage:
		return model.Damage{Source: entity(ev.Source), Target: entity(ev.Target), Amount: ev.Amount, Crit: ev.Crit, Miss: ev.Miss, Dodge: ev.Dodge, Block: ev.Block, Status: ev.Status, Shielded: ev.Shielded}, true
	case world.SkillUsed:
		return model.SkillUsed{Caster: ev.Caster.String(), Skill: ev.Skill, Stage: ev.Stage, Trait: ev.Trait, Aim: ev.Aim, Target: lockOf(ev.Target)}, true
	case world.AttackUsed:
		return model.AttackUsed{Attacker: ev.Attacker.String(), LengthTicks: int(ev.Length), Aim: ev.Aim}, true //nolint:gosec // lengths are small
	case world.MobAttacked:
		return model.MobAttacked{MobID: ev.Mob, Attack: ev.Attack, ToX: ev.ToX, Yaw: ev.Yaw, ToY: ev.ToY, ToZ: ev.ToZ}, true
	case world.Died:
		return model.Died{CharacterID: ev.ID.String(), X: ev.X, Y: ev.Y, Z: ev.Z}, true
	case world.Respawned:
		return model.Respawned{CharacterID: ev.ID.String(), MapID: ev.MapID, X: ev.X, Y: ev.Y, HP: ev.HP, Z: ev.Z, Yaw: ev.Yaw}, true
	case world.ExpGained:
		return model.ExpGained{Amount: ev.Amount, Exp: ev.Exp}, true
	case world.TeleportCancelled:
		return model.TeleportCancelled{Reason: teleportCancels[ev.Reason]}, true
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
		if ev.Timers != nil {
			sc.Retimed, sc.Timers = true, timersLeft(*ev.Timers, time.Now())
		}
		return sc, true
	case world.MatchState:
		st := model.MatchState{Match: ev.Match.String(), Mode: ev.Mode, Phase: model.MatchPhase(ev.Phase), Round: ev.Round, Wins: ev.Wins, EndsIn: ev.EndsIn}
		for s, side := range ev.Sides {
			for _, f := range side {
				st.Sides[s] = append(st.Sides[s], model.MatchFighter{ID: f.ID.String(), Name: f.Name, Class: f.Class.String(), Bot: f.Bot})
			}
		}
		return st, true
	case world.Loot:
		items := make([]model.LootItem, len(ev.Items))
		for i, it := range ev.Items {
			items[i] = model.LootItem(it)
		}
		return model.Loot{Kind: lootKinds[ev.Kind], MapID: ev.MapID, Pouch: ev.Pouch, Owner: ev.Owner.String(), OwnerName: ev.OwnerName, Items: items}, true
	case world.NpcDialog:
		return npcDialog(ev), true
	case world.QuestChanged:
		return model.QuestChanged{Change: questChanges[ev.Change], Quest: questState(ev.ID, ev.Quest)}, true
	case world.Frame, world.Detached:
	}
	return nil, false
}

func fieldBoss(b world.FieldBoss) model.FieldBoss {
	return model.FieldBoss{MobID: b.Mob, Type: b.Type}
}

func bossGone(b world.FieldBossGone) model.FieldBossGone {
	return model.FieldBossGone{MobID: b.Mob, Type: b.Type, BackInMs: b.BackIn.Milliseconds()}
}

// lootKinds, pouchKinds, questChanges, questStatuses, offerStates, conditionKinds, mobPoses and teleportCancels are the world's enums as the gateway's; an unknown value is the zero one.
var (
	lootKinds = map[world.LootKind]model.LootKind{
		world.LootTaken: model.LootTaken,
		world.LootLeft:  model.LootLeft,
	}
	pouchKinds = map[world.PouchKind]model.PouchKind{
		world.PouchSmall: model.PouchSmall,
		world.PouchBox:   model.PouchBox,
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
	teleportCancels = map[world.TeleportCancel]model.TeleportCancel{
		world.TeleportMoved: model.TeleportMoved,
		world.TeleportActed: model.TeleportActed,
		world.TeleportHit:   model.TeleportHit,
		world.TeleportDied:  model.TeleportDied,
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

// lockOf is r as a lock, the zero Entity for none.
func lockOf(r world.EntityRef) model.Entity {
	if r == (world.EntityRef{}) {
		return model.Entity{}
	}
	return entity(r)
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
		PrimarySkills: stats(s.PrimarySkills), DerivedSkills: derived(s.DerivedSkills), Skills: model.SkillPoints{Unspent: s.Skills.Unspent, Invested: s.Skills.Points, Traits: s.Skills.Traits},
		Points:            model.Points{Unspent: s.Points.Unspent, Spent: stats(s.Points.Spent), FreeResetLeft: !s.Points.FreeResetUsed},
		AttackLengthTicks: int(s.AttackLengthTicks), //nolint:gosec // an attack is a few ticks
		Buffs:             buffViews(s.Buffs),
	}
}

func buffViews(bs []world.BuffView) []model.BuffView {
	out := make([]model.BuffView, 0, len(bs))
	for _, b := range bs {
		v := model.BuffView{Skill: b.Skill, Left: b.Left, Points: b.Points}
		if !b.From.IsZero() {
			v.From = b.From.String()
		}
		out = append(out, v)
	}
	return out
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
	out := &model.Frame{Parts: make([]model.Part, len(f.Parts))}
	for i, p := range f.Parts {
		out.Parts[i] = model.Part{
			Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), RemovedPlayers: ids(p.Removed),
			RemovedMobs: p.RemovedMobs, Boss: mobView(p.Boss),
		}
	}
	return out
}

func unseenOf(u *world.Unseen) *model.Unseen {
	if u == nil {
		return nil
	}
	return &model.Unseen{Players: ids(u.Players), Mobs: u.Mobs}
}

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

func playerViews(ps []world.PlayerView) []model.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]model.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, model.PlayerView{
			ID: p.ID.String(), Name: p.Name, Class: p.Class.String(), Level: p.Level,
			X: p.State.X, Y: p.State.Y, VX: p.State.VX, VY: p.State.VY,
			OnGround: p.State.OnGround, HP: p.HP, MaxHP: p.MaxHP, Dead: p.Dead,
			Invulnerable: p.Invulnerable, Z: p.State.Z, VZ: p.State.VZ, HX: p.State.HX, HZ: p.State.HZ, Aim: p.Aim,
			Move:     model.PlayerMove{VX: p.State.Move.VX, VZ: p.State.Move.VZ, Left: p.State.Move.Left, Stop: p.State.Move.Stop, Through: p.State.Move.Through, ClearX: p.State.Move.ClearX, ClearZ: p.State.Move.ClearZ},
			Dodging:  p.Dodging,
			Shield:   p.Shield,
			Held:     p.State.Held,
			Sprint:   p.State.Sprint,
			Walk:     p.State.Walk,
			Look:     model.Look(p.Look),
			Slow:     model.PlayerSlow{Left: p.State.Slow.Left, Factor: p.State.Slow.Factor},
			Statuses: statusViews(p.Statuses),
			Team:     p.Team,
			Bot:      p.Bot,
		})
	}
	return out
}

func statusViews(ss []world.StatusView) []model.StatusView {
	if len(ss) == 0 {
		return nil
	}
	out := make([]model.StatusView, len(ss))
	for i, s := range ss {
		out[i] = model.StatusView(s)
	}
	return out
}

func mobViews(ms []world.MobView) []model.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]model.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, mobViewOf(m))
	}
	return out
}

func mobView(m *world.MobView) *model.MobView {
	if m == nil {
		return nil
	}
	v := mobViewOf(*m)
	return &v
}

func mobViewOf(m world.MobView) model.MobView {
	return model.MobView{
		ID: m.ID, Type: m.Type, X: m.X, Y: m.Y, HP: m.HP, MaxHP: m.MaxHP, Pose: mobPoses[m.Pose], Z: m.Z, Yaw: m.Yaw,
		Statuses: statusViews(m.Statuses),
	}
}

func pouchViews(ps []world.PouchView) []model.PouchView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]model.PouchView, 0, len(ps))
	for _, p := range ps {
		items := make([]model.PouchItem, 0, len(p.Items))
		for _, it := range p.Items {
			items = append(items, model.PouchItem{ID: it.ID, Template: it.Template, Quantity: it.Quantity, Stats: rollOf(it.Stats)})
		}
		out = append(out, model.PouchView{ID: p.ID, Kind: pouchKinds[p.Kind], X: p.X, Y: p.Y, Z: p.Z, Items: items, GoneInMs: p.GoneIn.Milliseconds()})
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
