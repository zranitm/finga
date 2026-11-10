package handler

import (
	"testing"

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
		DerivedWorn: progression.Derived{MaxHP: 30, PhysicalAttack: 12.5, PhysicalDefense: 3, Penetration: 0.05},
	}
	points := model.Points{Spent: gamedata.Stats{STR: 4}, Unspent: 1, FreeResetUsed: true}
	if !s.Send(model.StatsChanged{Tick: 5, Sheet: model.Sheet{Sheet: sheet, Points: points, AttackCooldownTicks: 12}, HP: 200, MaxHP: 234, MP: 20, MaxMP: 28}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 5, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
		Sheet: &worldv1.StatSheet{
			Primary: &worldv1.Stats{Strength: 16, Vitality: 10}, PrimaryWorn: &worldv1.Stats{Strength: 4},
			Derived: &worldv1.DerivedStats{
				PhysicalAttack: 33.5, MagicAttack: 8.5, PhysicalDefense: 18, MagicDefense: 10, Accuracy: 21, Evasion: 9, CritRate: 0.06, CritDamage: 0.525,
				Penetration: 0.05, HpRegen: 0.5, MpRegen: 0.2, AttackSpeed: 0.016, CooldownSpeed: 0.008, MoveSpeed: 0.007, MaxHp: 234, MaxMp: 28,
			},
			DerivedWorn:         &worldv1.DerivedStats{PhysicalAttack: 12.5, PhysicalDefense: 3, Penetration: 0.05, MaxHp: 30},
			Points:              &worldv1.Points{Unspent: 1, Spent: &worldv1.Stats{Strength: 4}, FreeResetLeft: false},
			AttackCooldownTicks: 12,
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
			{Quest: "d", State: model.OfferUnmet, Unmet: model.Unmet{Level: 5, Class: kernel.Bowman, Quests: []string{"a"}}},
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
