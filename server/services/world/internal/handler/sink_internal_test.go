package handler

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/hub"
	"finga/services/world/internal/model"
)

// A tick's frame reaches its gateway's stream once, whole, with each recipient's own ack.
func TestAFrameGoesToItsGatewayOnce(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](8).Subscribe("gw")
	a, b := &streamSink{sub: sub, character: "a", conn: "c-a"}, &streamSink{sub: sub, character: "b", conn: "c-b"}
	if a.Stream() != b.Stream() {
		t.Fatal("two players on one gateway have two streams")
	}
	frame := &model.Frame{
		Tick: 3, Players: []model.PlayerView{{ID: kernel.NewCharacterID(), Name: "Ayla"}}, Mobs: []model.MobView{{ID: 4}},
		Drops: []model.DropView{{ID: 5, Owner: kernel.NewCharacterID()}}, Removed: []kernel.CharacterID{kernel.NewCharacterID()}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{7},
		CollectedDrops: []uint64{8},
	}
	ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
	if !a.Stream().SendFrame(frame, []model.Recipient{{Character: ayla, Connection: "c-a", AckSeq: 8}, {Character: robin, Connection: "c-b"}}) {
		t.Fatal("the stream refused the frame")
	}
	if n := len(sub.C()); n != 1 {
		t.Fatalf("%d messages on the stream, want 1", n)
	}
	got := <-sub.C()
	want := &worldv1.DeliverResponse{Tick: 3, Event: &worldv1.DeliverResponse_Frame{Frame: &worldv1.Frame{
		Delta: encodeFrame(frame),
		Recipients: []*worldv1.Recipient{
			{CharacterId: ayla.String(), ConnectionId: "c-a", AckSeq: 8},
			{CharacterId: robin.String(), ConnectionId: "c-b"},
		},
	}}}
	if !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if d := got.GetFrame().GetDelta(); len(d.GetPlayers()) != 1 || len(d.GetMobs()) != 1 || len(d.GetDrops()) != 1 || d.GetDrops()[0].GetOwnerId() != frame.Drops[0].Owner.String() ||
		len(d.GetRemovedPlayers()) != 1 || len(d.GetRemovedMobs()) != 1 || len(d.GetRemovedDrops()) != 1 || len(d.GetCollectedDrops()) != 1 {
		t.Fatalf("the frame lost a part: %v", d)
	}
}

// A gateway whose stream is full refuses the frame, which drops its players.
func TestAFullStreamRefusesTheFrame(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	if !s.Send(model.Detached{}) {
		t.Fatal("the first message was refused")
	}
	if s.Stream().SendFrame(&model.Frame{}, []model.Recipient{{Character: kernel.NewCharacterID()}}) {
		t.Fatal("a full stream took the frame")
	}
}

// An inventory change goes to its gateway with the revision it took, so the client can tell it from an older view of its slots.
func TestAnInventoryChangeCarriesItsRevision(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	item := kernel.NewItemID()
	if !s.Send(model.InventoryChanged{Tick: 9, Revision: 14, Slots: []model.Slot{{Tab: "etc", Slot: 2, Item: &model.SlotItem{ID: item, Template: "slime_gel", Quantity: 3}}, {Tab: "etc", Slot: 4}}}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 9, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
		Revision: 14,
		Slots: []*worldv1.Slot{
			{Tab: "etc", Slot: 2, Item: &worldv1.Item{Id: item.String(), TemplateId: "slime_gel", Tab: "etc", Slot: 2, Quantity: 3}},
			{Tab: "etc", Slot: 4},
		},
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// An equip's change carries the equip slots and each equipment item's roll.
func TestAnEquipChangeCarriesItsEquipSlotsAndRolls(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	hat := &model.SlotItem{ID: kernel.NewItemID(), Template: "leather_cap", Quantity: 1, Stats: &progression.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}}
	if !s.Send(model.InventoryChanged{Tick: 4, Revision: 7, Slots: []model.Slot{{Tab: "equip", Slot: 0}}, Equipment: []model.EquipSlot{{Slot: "hat", Item: hat}, {Slot: "weapon"}}}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 4, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
		Revision: 7,
		Slots:    []*worldv1.Slot{{Tab: "equip", Slot: 0}},
		Equipment: []*worldv1.EquipSlot{
			{EquipSlot: "hat", Item: &worldv1.Item{Id: hat.ID.String(), TemplateId: "leather_cap", Quantity: 1, Stats: &worldv1.ItemStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, MaxMp: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HpRegen: 17, MpRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}}},
			{EquipSlot: "weapon"},
		},
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A change of HP or MP alone leaves the sheet unset, not zeroed, so the gateway and the client keep the last one.
func TestStatsChangedWithoutASheetLeavesItUnset(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	if !s.Send(model.StatsChanged{Tick: 6, HP: 200, MaxHP: 234, MP: 21, MaxMP: 28}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 6, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{Hp: 200, MaxHp: 234, Mp: 21, MaxMp: 28}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Every stat of the sheet, the points and the cooldown reach world.v1, the free reset as the one left.
func TestStatsChangedCarriesTheSheet(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	sheet := progression.Sheet{
		Primary: gamedata.Stats{STR: 16, VIT: 10}, PrimaryWorn: gamedata.Stats{STR: 4},
		Derived: progression.Derived{
			MaxHP: 234, MaxMP: 28, PhysicalAttack: 33.5, MagicAttack: 8.5, PhysicalDefense: 18, MagicDefense: 10, Accuracy: 21, Evasion: 9, CritRate: 0.06, CritDamage: 0.525,
			Penetration: 0.05, HPRegen: 0.5, MPRegen: 0.2, AttackSpeed: 0.016, CooldownSpeed: 0.008, MoveSpeed: 0.007,
		},
		DerivedWorn:   progression.Derived{MaxHP: 30, PhysicalAttack: 12.5, PhysicalDefense: 3, Penetration: 0.05},
		DerivedSkills: progression.Derived{HPRegen: 0.3},
	}
	points := model.Points{Spent: gamedata.Stats{STR: 4}, Unspent: 1, FreeResetUsed: true}
	skills := model.Skills{Points: map[string]int{"second_wind": 2, "strike": 5}, Unspent: 3}
	if !s.Send(model.StatsChanged{Tick: 5, Sheet: &model.Sheet{Sheet: sheet, Points: points, Skills: skills, AttackLengthTicks: 12}, HP: 200, MaxHP: 234, MP: 20, MaxMP: 28}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 5, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
		Sheet: &worldv1.StatSheet{
			Primary: &worldv1.Stats{Strength: 16, Vitality: 10}, PrimaryWorn: &worldv1.Stats{Strength: 4},
			Derived: &worldv1.DerivedStats{
				PhysicalAttack: 33.5, MagicAttack: 8.5, PhysicalDefense: 18, MagicDefense: 10, Accuracy: 21, Evasion: 9, CritRate: 0.06, CritDamage: 0.525,
				Penetration: 0.05, HpRegen: 0.5, MpRegen: 0.2, AttackSpeed: 0.016, CooldownSpeed: 0.008, MoveSpeed: 0.007, MaxHp: 234, MaxMp: 28,
			},
			DerivedWorn:       &worldv1.DerivedStats{PhysicalAttack: 12.5, PhysicalDefense: 3, Penetration: 0.05, MaxHp: 30},
			Points:            &worldv1.Points{Unspent: 1, Spent: &worldv1.Stats{Strength: 4}, FreeResetLeft: false},
			AttackLengthTicks: 12,
			PrimarySkills:     &worldv1.Stats{},
			DerivedSkills:     &worldv1.DerivedStats{HpRegen: 0.3},
			Skills:            &worldv1.SkillPoints{Unspent: 3, Invested: map[string]int32{"second_wind": 2, "strike": 5}},
		},
		Hp: 200, MaxHp: 234, Mp: 20, MaxMp: 28,
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Loot tells what became of a drop, naming its owner, so a party member's line can say who got it.
func TestLootCarriesWhatBecameOfTheDrop(t *testing.T) {
	owner := kernel.NewCharacterID()
	for kind, want := range map[model.LootKind]worldv1.LootKind{
		model.LootCollected: worldv1.LootKind_LOOT_KIND_COLLECTED,
		model.LootWaiting:   worldv1.LootKind_LOOT_KIND_WAITING,
		model.LootLost:      worldv1.LootKind_LOOT_KIND_LOST,
		0:                   worldv1.LootKind_LOOT_KIND_UNSPECIFIED,
	} {
		sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
		s := &streamSink{sub: sub, character: "a", conn: "c-a"}
		if !s.Send(model.Loot{Tick: 6, Kind: kind, MapID: "forest", DropID: 9, Owner: owner, OwnerName: "Robin", Template: "slime_gel", Quantity: 2}) {
			t.Fatal("the stream refused the loot")
		}
		got := <-sub.C()
		wantMsg := &worldv1.DeliverResponse{CharacterId: "a", ConnectionId: "c-a", Tick: 6, Event: &worldv1.DeliverResponse_Loot{Loot: &worldv1.Loot{
			Kind: want, MapId: "forest", DropId: 9, OwnerId: owner.String(), OwnerName: "Robin", Template: "slime_gel", Quantity: 2,
		}}}
		if !proto.Equal(got, wantMsg) {
			t.Fatalf("got %v, want %v", got, wantMsg)
		}
	}
}

func TestNpcDialogCarriesEachOfferAndTurnIn(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub, character: "a", conn: "c-a"}
	if !s.Send(model.NpcDialog{Tick: 4, NPC: "guide",
		Gives: []model.QuestOffer{
			{Quest: "a", State: model.OfferAvailable},
			{Quest: "b", State: model.OfferActive},
			{Quest: "c", State: model.OfferFinished},
			{Quest: "d", State: model.OfferUnmet, Unmet: model.Unmet{Level: 5, Class: kernel.Class("bowman"), Quests: []string{"a"}}},
		},
		Takes: []model.QuestTurnIn{{Quest: "b", Ready: true}},
	}) {
		t.Fatal("the stream refused the dialog")
	}
	want := &worldv1.DeliverResponse{CharacterId: "a", ConnectionId: "c-a", Tick: 4, Event: &worldv1.DeliverResponse_NpcDialog{NpcDialog: &worldv1.NpcDialog{
		NpcId: "guide",
		Gives: []*worldv1.QuestOffer{
			{QuestId: "a", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
			{QuestId: "b", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE},
			{QuestId: "c", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED},
			{QuestId: "d", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &worldv1.QuestConditions{Level: 5, Class: "bowman", Quests: []string{"a"}}},
		},
		Takes: []*worldv1.QuestTurnIn{{QuestId: "b", Ready: true}},
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSkillUsedCarriesTheCasterSkillStageAndFacing(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub, character: "a", conn: "c-a"}
	caster := kernel.NewCharacterID()
	if !s.Send(model.SkillUsed{Tick: 3, Caster: caster, Skill: "strike", Stage: 2, Facing: -1}) {
		t.Fatal("the stream refused the event")
	}
	want := &worldv1.DeliverResponse{CharacterId: "a", ConnectionId: "c-a", Tick: 3, Event: &worldv1.DeliverResponse_SkillUsed{SkillUsed: &worldv1.SkillUsed{Caster: caster.String(), Skill: "strike", Stage: 2, Facing: -1}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestATrainersDialogCarriesItsPromotion(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub, character: "a", conn: "c-a"}
	if !s.Send(model.NpcDialog{Tick: 4, NPC: "warrior_trainer", Promotion: &model.PromotionOffer{Class: "warrior", Conditions: []model.Condition{
		{Kind: model.ConditionLevel, Need: 10, Have: 9},
		{Kind: model.ConditionStat, ID: "str", Need: 30, Have: 30},
		{Kind: model.ConditionSkill, ID: "strike", Need: 10, Have: 4},
		{Kind: model.ConditionQuest, ID: "warriors_trial", Need: 1, Have: 1},
	}}}) {
		t.Fatal("the stream refused the dialog")
	}
	want := &worldv1.DeliverResponse{CharacterId: "a", ConnectionId: "c-a", Tick: 4, Event: &worldv1.DeliverResponse_NpcDialog{NpcDialog: &worldv1.NpcDialog{
		NpcId: "warrior_trainer",
		Promotion: &worldv1.PromotionOffer{Class: "warrior", Conditions: []*worldv1.PromotionCondition{
			{Kind: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL, Need: 10, Have: 9},
			{Kind: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_STAT, Id: "str", Need: 30, Have: 30},
			{Kind: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL, Id: "strike", Need: 10, Have: 4},
			{Kind: worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_QUEST, Id: "warriors_trial", Need: 1, Have: 1},
		}},
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPromotedCarriesTheCharacterInItsNewClass(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub, character: "a", conn: "c-a"}
	id := kernel.NewCharacterID()
	if !s.Send(model.Promoted{Tick: 6, ID: id, Name: "Ayla", Class: "warrior", Level: 10, Exp: 5, HP: 300, MaxHP: 300, MP: 40, MaxMP: 40}) {
		t.Fatal("the stream refused the event")
	}
	got := (<-sub.C()).GetPromoted().GetCharacter()
	if got.GetId() != id.String() || got.GetClass() != "warrior" || got.GetLevel() != 10 || got.GetHp() != 300 || got.GetMaxMp() != 40 || got.GetSheet() == nil {
		t.Fatalf("promoted %v", got)
	}
}

func TestQuestChangedCarriesTheQuestAsItStands(t *testing.T) {
	for _, c := range []struct {
		in   model.QuestChanged
		want *worldv1.QuestChanged
	}{
		{model.QuestChanged{Tick: 3, ID: "a", Change: model.QuestProgressed, Quest: model.Quest{Status: model.QuestActive, Progress: []int{4, 0}, TimesFinished: 1}},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_PROGRESSED, Quest: &worldv1.QuestState{QuestId: "a", Status: worldv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{4, 0}, TimesFinished: 1}}},
		{model.QuestChanged{Tick: 3, ID: "b", Change: model.QuestHandedIn, Quest: model.Quest{Status: model.QuestFinished, TimesFinished: 2}},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_HANDED_IN, Quest: &worldv1.QuestState{QuestId: "b", Status: worldv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 2}}},
		{model.QuestChanged{Tick: 3, ID: "c", Change: model.QuestAbandoned},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_ABANDONED, Quest: &worldv1.QuestState{QuestId: "c"}}},
		{model.QuestChanged{Tick: 3, ID: "d", Change: model.QuestAccepted, Quest: model.Quest{Status: model.QuestActive, Progress: []int{0}}},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_ACCEPTED, Quest: &worldv1.QuestState{QuestId: "d", Status: worldv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}}}},
	} {
		sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
		s := &streamSink{sub: sub, character: "a", conn: "c-a"}
		if !s.Send(c.in) {
			t.Fatal("the stream refused the change")
		}
		want := &worldv1.DeliverResponse{CharacterId: "a", ConnectionId: "c-a", Tick: 3, Event: &worldv1.DeliverResponse_QuestChanged{QuestChanged: c.want}}
		if got := <-sub.C(); !proto.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// The field boss goes out as it appears, in a joining player's Snapshot, and as it goes, its time left in milliseconds.
func TestTheFieldBossEventsCarryItsMobTypeAndTime(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](3).Subscribe("gw")
	s := &streamSink{sub: sub, character: "a", conn: "c-a"}
	up := model.FieldBoss{Tick: 5, Mob: 9, Type: "king_slime", LeavesIn: 90 * time.Second}
	wantUp := &worldv1.FieldBoss{MobId: 9, Type: "king_slime", LeavesInMs: 90000}
	for _, e := range []model.Event{up, model.Snapshot{Tick: 6, MapID: "forest", FieldBoss: &up}, model.FieldBossGone{Tick: 7, Mob: 9, Beaten: true}} {
		if !s.Send(e) {
			t.Fatal("the stream refused the event")
		}
	}
	for _, want := range []*worldv1.DeliverResponse{
		{CharacterId: "a", ConnectionId: "c-a", Tick: 5, Event: &worldv1.DeliverResponse_FieldBoss{FieldBoss: wantUp}},
		{CharacterId: "a", ConnectionId: "c-a", Tick: 6, Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{MapId: "forest", FieldBoss: wantUp}}},
		{CharacterId: "a", ConnectionId: "c-a", Tick: 7, Event: &worldv1.DeliverResponse_FieldBossGone{FieldBossGone: &worldv1.FieldBossGone{MobId: 9, Beaten: true}}},
	} {
		if got := <-sub.C(); !proto.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
