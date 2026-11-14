package handler

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/pkg/hub"
	"finga/services/world"
)

// playerA is the player the tests' deliveries go to.
var playerA = kernel.NewCharacterID()

// sink is a player's place on its gateway's stream, standing in for the facade's: each event goes out as the handler sends it.
type sink struct {
	sub       *hub.Sub[*worldv1.DeliverResponse]
	character kernel.CharacterID
	conn      string
}

func (s sink) Send(tick uint64, e world.Event) bool {
	return s.sub.Send(deliverResponse(world.Delivery{Character: s.character, Conn: s.conn, Tick: tick, Event: e}))
}

// A tick's frame goes out once, whole, with each recipient's own ack and no player of its own.
func TestAFrameGoesOutOnceWithItsRecipients(t *testing.T) {
	frame := world.Frame{
		Players: []world.PlayerView{{ID: kernel.NewCharacterID(), Name: "Ayla", State: sim.State{Facing: -1, Casting: 42}}}, Mobs: []world.MobView{{ID: 4}},
		Drops: []world.DropView{{ID: 5, Owner: kernel.NewCharacterID()}}, Removed: []kernel.CharacterID{kernel.NewCharacterID()}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{7},
		CollectedDrops: []uint64{8},
	}
	ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
	frame.Recipients = []world.Recipient{{Character: ayla, Connection: "c-a", AckSeq: 8}, {Character: robin, Connection: "c-b"}}
	got := deliverResponse(world.Delivery{Tick: 3, Event: frame})
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
	if d := got.GetFrame().GetDelta(); len(d.GetPlayers()) != 1 || d.GetPlayers()[0].GetCasting() != 42 || len(d.GetMobs()) != 1 || len(d.GetDrops()) != 1 || d.GetDrops()[0].GetOwnerId() != frame.Drops[0].Owner.String() ||
		len(d.GetRemovedPlayers()) != 1 || len(d.GetRemovedMobs()) != 1 || len(d.GetRemovedDrops()) != 1 || len(d.GetCollectedDrops()) != 1 {
		t.Fatalf("the frame lost a part: %v", d)
	}
}

// A mob's pose goes out with it, so a player who joins sees a downed mob.
func TestAMobViewCarriesItsPose(t *testing.T) {
	for pose, want := range map[world.MobPose]worldv1.MobPose{
		world.Standing:  worldv1.MobPose_MOB_POSE_UNSPECIFIED,
		world.Flinching: worldv1.MobPose_MOB_POSE_FLINCH,
		world.Down:      worldv1.MobPose_MOB_POSE_DOWN,
		world.Rising:    worldv1.MobPose_MOB_POSE_RISING,
	} {
		if got := encodeFrame(world.Frame{Mobs: []world.MobView{{ID: 4, Pose: pose}}}).GetMobs()[0].GetPose(); got != want {
			t.Errorf("pose %d went out as %v, want %v", pose, got, want)
		}
	}
}

// An inventory change goes to its gateway with the revision it took, so the client can tell it from an older view of its slots.
func TestAnInventoryChangeCarriesItsRevision(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := sink{sub: sub}
	item := kernel.NewItemID()
	if !s.Send(9, world.InventoryChanged{Revision: 14, Slots: []world.Slot{{Tab: "etc", Slot: 2, Item: &world.SlotItem{ID: item, Template: "slime_gel", Quantity: 3}}, {Tab: "etc", Slot: 4}}}) {
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
	s := sink{sub: sub}
	hat := &world.SlotItem{ID: kernel.NewItemID(), Template: "leather_cap", Quantity: 1, Stats: &progression.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, CooldownSpeed: 19, MoveSpeed: 20}}
	if !s.Send(4, world.InventoryChanged{Revision: 7, Slots: []world.Slot{{Tab: "equip", Slot: 0}}, Equipment: []world.EquipSlot{{Slot: "hat", Item: hat}, {Slot: "weapon"}}}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 4, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
		Revision: 7,
		Slots:    []*worldv1.Slot{{Tab: "equip", Slot: 0}},
		Equipment: []*worldv1.EquipSlot{
			{EquipSlot: "hat", Item: &worldv1.Item{Id: hat.ID.String(), TemplateId: "leather_cap", Quantity: 1, Stats: &worldv1.ItemStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, MaxMp: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HpRegen: 17, MpRegen: 18, CooldownSpeed: 19, MoveSpeed: 20}}},
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
	s := sink{sub: sub}
	if !s.Send(6, world.StatsChanged{HP: 200, MaxHP: 234, MP: 21, MaxMP: 28}) {
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
	s := sink{sub: sub}
	sheet := progression.Sheet{
		Primary: gamedata.Stats{STR: 16, VIT: 10}, PrimaryWorn: gamedata.Stats{STR: 4},
		Derived: progression.Derived{
			MaxHP: 234, MaxMP: 28, PhysicalAttack: 33.5, MagicAttack: 8.5, PhysicalDefense: 18, MagicDefense: 10, Accuracy: 21, Evasion: 9, CritRate: 0.06, CritDamage: 0.525,
			Penetration: 0.05, HPRegen: 0.5, MPRegen: 0.2, CooldownSpeed: 0.008, MoveSpeed: 0.007,
		},
		DerivedWorn:   progression.Derived{MaxHP: 30, PhysicalAttack: 12.5, PhysicalDefense: 3, Penetration: 0.05},
		DerivedSkills: progression.Derived{HPRegen: 0.3},
	}
	points := world.Points{Spent: gamedata.Stats{STR: 4}, Unspent: 1, FreeResetUsed: true}
	skills := world.Skills{Points: map[string]int{"second_wind": 2, "strike": 5}, Unspent: 3}
	if !s.Send(5, world.StatsChanged{Sheet: &world.Sheet{Sheet: sheet, Points: points, Skills: skills, AttackLengthTicks: 12}, HP: 200, MaxHP: 234, MP: 20, MaxMP: 28}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 5, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
		Sheet: &worldv1.StatSheet{
			Primary: &worldv1.Stats{Strength: 16, Vitality: 10}, PrimaryWorn: &worldv1.Stats{Strength: 4},
			Derived: &worldv1.DerivedStats{
				PhysicalAttack: 33.5, MagicAttack: 8.5, PhysicalDefense: 18, MagicDefense: 10, Accuracy: 21, Evasion: 9, CritRate: 0.06, CritDamage: 0.525,
				Penetration: 0.05, HpRegen: 0.5, MpRegen: 0.2, CooldownSpeed: 0.008, MoveSpeed: 0.007, MaxHp: 234, MaxMp: 28,
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
	for kind, want := range map[world.LootKind]worldv1.LootKind{
		world.LootCollected: worldv1.LootKind_LOOT_KIND_COLLECTED,
		world.LootWaiting:   worldv1.LootKind_LOOT_KIND_WAITING,
		world.LootLost:      worldv1.LootKind_LOOT_KIND_LOST,
		0:                   worldv1.LootKind_LOOT_KIND_UNSPECIFIED,
	} {
		sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
		s := sink{sub: sub, character: playerA, conn: "c-a"}
		if !s.Send(6, world.Loot{Kind: kind, MapID: "forest", DropID: 9, Owner: owner, OwnerName: "Robin", Template: "slime_gel", Quantity: 2}) {
			t.Fatal("the stream refused the loot")
		}
		got := <-sub.C()
		wantMsg := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 6, Event: &worldv1.DeliverResponse_Loot{Loot: &worldv1.Loot{
			Kind: want, MapId: "forest", DropId: 9, OwnerId: owner.String(), OwnerName: "Robin", Template: "slime_gel", Quantity: 2,
		}}}
		if !proto.Equal(got, wantMsg) {
			t.Fatalf("got %v, want %v", got, wantMsg)
		}
	}
}

func TestNpcDialogCarriesEachOfferAndTurnIn(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	if !s.Send(4, world.NpcDialog{NPC: "guide",
		Gives: []world.QuestOffer{
			{Quest: "a", State: world.OfferAvailable},
			{Quest: "b", State: world.OfferActive},
			{Quest: "c", State: world.OfferFinished},
			{Quest: "d", State: world.OfferUnmet, Unmet: world.Unmet{Level: 5, Class: kernel.Class("bowman"), Quests: []string{"a"}}},
		},
		Takes: []world.QuestTurnIn{{Quest: "b", Ready: true}},
	}) {
		t.Fatal("the stream refused the dialog")
	}
	want := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 4, Event: &worldv1.DeliverResponse_NpcDialog{NpcDialog: &worldv1.NpcDialog{
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
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	caster := kernel.NewCharacterID()
	if !s.Send(3, world.SkillUsed{Caster: caster, Skill: "strike", Stage: 2, Facing: -1}) {
		t.Fatal("the stream refused the event")
	}
	want := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 3, Event: &worldv1.DeliverResponse_SkillUsed{SkillUsed: &worldv1.SkillUsed{Caster: caster.String(), Skill: "strike", Stage: 2, Facing: -1}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAttackUsedCarriesTheAttackerFacingAndLength(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	attacker := kernel.NewCharacterID()
	if !s.Send(3, world.AttackUsed{Attacker: attacker, Facing: -1, Length: 18}) {
		t.Fatal("the stream refused the event")
	}
	want := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 3, Event: &worldv1.DeliverResponse_AttackUsed{AttackUsed: &worldv1.AttackUsed{Attacker: attacker.String(), Facing: -1, LengthTicks: 18}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMobAttackedCarriesTheMobItsAttackAndFacing(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	if !s.Send(3, world.MobAttacked{Mob: 7, Attack: 1, Facing: -1}) {
		t.Fatal("the stream refused the event")
	}
	want := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 3, Event: &worldv1.DeliverResponse_MobAttacked{MobAttacked: &worldv1.MobAttacked{Mob: 7, Attack: 1, Facing: -1}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestATrainersDialogCarriesItsPromotion(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	if !s.Send(4, world.NpcDialog{NPC: "warrior_trainer", Promotion: &world.PromotionOffer{Class: "warrior", Conditions: []world.Condition{
		{Kind: world.ConditionLevel, Need: 10, Have: 9},
		{Kind: world.ConditionStat, ID: "str", Need: 30, Have: 30},
		{Kind: world.ConditionSkill, ID: "strike", Need: 10, Have: 4},
		{Kind: world.ConditionQuest, ID: "warriors_trial", Need: 1, Have: 1},
	}}}) {
		t.Fatal("the stream refused the dialog")
	}
	want := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 4, Event: &worldv1.DeliverResponse_NpcDialog{NpcDialog: &worldv1.NpcDialog{
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
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	id := kernel.NewCharacterID()
	if !s.Send(6, world.Promoted{ID: id, Name: "Ayla", Class: "warrior", Level: 10, Exp: 5, HP: 300, MaxHP: 300, MP: 40, MaxMP: 40}) {
		t.Fatal("the stream refused the event")
	}
	got := (<-sub.C()).GetPromoted().GetCharacter()
	if got.GetId() != id.String() || got.GetClass() != "warrior" || got.GetLevel() != 10 || got.GetHp() != 300 || got.GetMaxMp() != 40 || got.GetSheet() == nil {
		t.Fatalf("promoted %v", got)
	}
}

func TestQuestChangedCarriesTheQuestAsItStands(t *testing.T) {
	for _, c := range []struct {
		in   world.QuestChanged
		want *worldv1.QuestChanged
	}{
		{world.QuestChanged{ID: "a", Change: world.QuestProgressed, Quest: world.Quest{Status: world.QuestActive, Progress: []int{4, 0}, TimesFinished: 1}},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_PROGRESSED, Quest: &worldv1.QuestState{QuestId: "a", Status: worldv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{4, 0}, TimesFinished: 1}}},
		{world.QuestChanged{ID: "b", Change: world.QuestHandedIn, Quest: world.Quest{Status: world.QuestFinished, TimesFinished: 2}},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_HANDED_IN, Quest: &worldv1.QuestState{QuestId: "b", Status: worldv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 2}}},
		{world.QuestChanged{ID: "c", Change: world.QuestAbandoned},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_ABANDONED, Quest: &worldv1.QuestState{QuestId: "c"}}},
		{world.QuestChanged{ID: "d", Change: world.QuestAccepted, Quest: world.Quest{Status: world.QuestActive, Progress: []int{0}}},
			&worldv1.QuestChanged{Change: worldv1.QuestChange_QUEST_CHANGE_ACCEPTED, Quest: &worldv1.QuestState{QuestId: "d", Status: worldv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}}}},
	} {
		sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
		s := sink{sub: sub, character: playerA, conn: "c-a"}
		if !s.Send(3, c.in) {
			t.Fatal("the stream refused the change")
		}
		want := &worldv1.DeliverResponse{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 3, Event: &worldv1.DeliverResponse_QuestChanged{QuestChanged: c.want}}
		if got := <-sub.C(); !proto.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// The field boss goes out as it appears, in a joining player's Snapshot, and as it goes, its time left in milliseconds.
func TestTheFieldBossEventsCarryItsMobTypeAndTime(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](3).Subscribe("gw")
	s := sink{sub: sub, character: playerA, conn: "c-a"}
	up := world.FieldBoss{Mob: 9, Type: "king_slime", LeavesIn: 90 * time.Second}
	wantUp := &worldv1.FieldBoss{MobId: 9, Type: "king_slime", LeavesInMs: 90000}
	for i, e := range []world.Event{up, world.Snapshot{MapID: "forest", FieldBoss: &up}, world.FieldBossGone{Mob: 9, Beaten: true}} {
		if !s.Send(uint64(5+i), e) { //nolint:gosec // a test's index
			t.Fatal("the stream refused the event")
		}
	}
	for _, want := range []*worldv1.DeliverResponse{
		{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 5, Event: &worldv1.DeliverResponse_FieldBoss{FieldBoss: wantUp}},
		{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 6, Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{MapId: "forest", FieldBoss: wantUp}}},
		{CharacterId: playerA.String(), ConnectionId: "c-a", Tick: 7, Event: &worldv1.DeliverResponse_FieldBossGone{FieldBossGone: &worldv1.FieldBossGone{MobId: 9, Beaten: true}}},
	} {
		if got := <-sub.C(); !proto.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
