package push

import (
	"strconv"

	clientv1 "finga/api/gen/client/v1"
	"finga/services/gateway/internal/model"
)

// InventoryChanged is the message for slots and equip slots an inventory command, a pickup or an equip changed.
func InventoryChanged(ic model.InventoryChanged) *clientv1.InventoryChanged {
	out := make([]*clientv1.InventorySlot, 0, len(ic.Slots))
	for _, s := range ic.Slots {
		slot := &clientv1.InventorySlot{Tab: s.Tab, Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = itemView(*s.Item)
		}
		out = append(out, slot)
	}
	return &clientv1.InventoryChanged{Slots: out, Revision: ic.Revision, Equipment: equipment(ic.Equipment)}
}

func statsChanged(sc model.StatsChanged) *clientv1.StatsChanged {
	out := &clientv1.StatsChanged{Hp: int32(sc.HP), MaxHp: int32(sc.MaxHP), Mp: int32(sc.MP), MaxMp: int32(sc.MaxMP)}
	if sc.Sheet != nil {
		out.Sheet = statSheet(*sc.Sheet)
	}
	return out
}

func characterView(c model.CharacterView) *clientv1.CharacterView {
	return &clientv1.CharacterView{
		Id: c.ID, Name: c.Name, Class: c.Class, Level: int32(c.Level), Exp: c.Exp, Stats: stats(c.Stats),
		Hp: int32(c.HP), MaxHp: int32(c.MaxHP), Mp: int32(c.MP), MaxMp: int32(c.MaxMP), Sheet: statSheet(c.Sheet),
	}
}

func stats(st model.Stats) *clientv1.Stats {
	return &clientv1.Stats{Str: int32(st.STR), Dex: int32(st.DEX), Int: int32(st.INT), Agi: int32(st.AGI), Vit: int32(st.VIT)}
}

func statSheet(s model.StatSheet) *clientv1.StatSheet {
	return &clientv1.StatSheet{
		Primary: stats(s.Primary), PrimaryWorn: stats(s.PrimaryWorn), Derived: derived(s.Derived), DerivedWorn: derived(s.DerivedWorn),
		PrimarySkills: stats(s.PrimarySkills), DerivedSkills: derived(s.DerivedSkills), Skills: skillPoints(s.Skills),
		Points:              &clientv1.Points{Unspent: int32(s.Points.Unspent), Spent: stats(s.Points.Spent), FreeResetLeft: s.Points.FreeResetLeft},
		AttackCooldownTicks: uint32(s.AttackCooldownTicks),
	}
}

func skillPoints(s model.SkillPoints) *clientv1.SkillPoints {
	out := &clientv1.SkillPoints{Unspent: int32(s.Unspent), Invested: make(map[string]int32, len(s.Invested))}
	for skill, n := range s.Invested {
		out.Invested[skill] = int32(n)
	}
	return out
}

func derived(d model.DerivedStats) *clientv1.DerivedStats {
	return &clientv1.DerivedStats{
		PhysicalAttack: d.PhysicalAttack, MagicAttack: d.MagicAttack, PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense,
		Accuracy: d.Accuracy, Evasion: d.Evasion, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration,
		HpRegen: d.HPRegen, MpRegen: d.MPRegen, AttackSpeed: d.AttackSpeed, CooldownSpeed: d.CooldownSpeed, MoveSpeed: d.MoveSpeed,
		MaxHp: int32(d.MaxHP), MaxMp: int32(d.MaxMP),
	}
}

func playerViews(ps []model.PlayerView) []*clientv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &clientv1.PlayerView{
			Id: p.ID, Name: p.Name, Class: p.Class, Level: int32(p.Level),
			X: p.X, Y: p.Y, Vx: p.VX, Vy: p.VY, Facing: int32(p.Facing),
			OnGround: p.OnGround, Climbing: p.Climbing, Hp: int32(p.HP), MaxHp: int32(p.MaxHP), Dead: p.Dead,
			Invulnerable: p.Invulnerable,
		})
	}
	return out
}

func mobViews(ms []model.MobView) []*clientv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*clientv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, &clientv1.MobView{
			Id: uint32(m.ID), Type: m.Type, X: m.X, Y: m.Y, Facing: int32(m.Facing), Hp: int32(m.HP), MaxHp: int32(m.MaxHP),
		})
	}
	return out
}

func dropViews(ds []model.DropView) []*clientv1.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]*clientv1.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, &clientv1.DropView{Id: uint32(d.ID), Template: d.Template, Quantity: int32(d.Quantity), X: d.X, Y: d.Y, OwnerId: d.Owner})
	}
	return out
}

// instanceIDs are mob or drop IDs, which count up within one map instance and fit the wire's 32 bits.
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

func itemViews(items []model.ItemView) []*clientv1.ItemView {
	out := make([]*clientv1.ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, itemView(it))
	}
	return out
}

func itemView(it model.ItemView) *clientv1.ItemView {
	v := &clientv1.ItemView{ItemId: it.ID, Template: it.Template, Tab: it.Tab, Slot: int32(it.Slot), Quantity: int32(it.Quantity)}
	if s := it.Stats; s != nil {
		v.Stats = &clientv1.ItemStats{
			Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
			PhysicalAttack: int32(s.PhysicalAttack), MagicAttack: int32(s.MagicAttack), PhysicalDefense: int32(s.PhysicalDefense), MagicDefense: int32(s.MagicDefense),
			MaxHp: int32(s.MaxHP), MaxMp: int32(s.MaxMP), Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion),
			CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), Penetration: int32(s.Penetration), HpRegen: int32(s.HPRegen), MpRegen: int32(s.MPRegen),
			AttackSpeed: int32(s.AttackSpeed), CooldownSpeed: int32(s.CooldownSpeed), MoveSpeed: int32(s.MoveSpeed),
		}
	}
	return v
}

func equipment(slots []model.EquipSlot) []*clientv1.EquipSlot {
	if len(slots) == 0 {
		return nil
	}
	out := make([]*clientv1.EquipSlot, 0, len(slots))
	for _, s := range slots {
		slot := &clientv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = itemView(*s.Item)
		}
		out = append(out, slot)
	}
	return out
}

func partyView(p *model.PartyView) *clientv1.PartyView {
	if p == nil {
		return nil
	}
	v := &clientv1.PartyView{Leader: p.Leader, Members: make([]*clientv1.PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		v.Members = append(v.Members, &clientv1.PartyMember{
			CharacterId: m.CharacterID, Name: m.Name, Level: int32(m.Level), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), MapId: m.MapID, Online: m.Online,
		})
	}
	return v
}

var chatChannels = map[model.ChatChannel]clientv1.ChatChannel{
	model.ChatMap:   clientv1.ChatChannel_CHAT_CHANNEL_MAP,
	model.ChatParty: clientv1.ChatChannel_CHAT_CHANNEL_PARTY,
}

var lootKinds = map[model.LootKind]clientv1.LootKind{
	model.LootCollected: clientv1.LootKind_LOOT_KIND_COLLECTED,
	model.LootWaiting:   clientv1.LootKind_LOOT_KIND_WAITING,
	model.LootLost:      clientv1.LootKind_LOOT_KIND_LOST,
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
	return out
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

// entityRef is how the client names a player or a mob: p:<character id> or m:<mob id>.
func entityRef(e model.Entity) string {
	if e.Character != "" {
		return "p:" + e.Character
	}
	return "m:" + strconv.FormatUint(e.Mob, 10)
}
