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
		Drops: []model.DropView{{ID: 5}}, Removed: []kernel.CharacterID{kernel.NewCharacterID()}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{7},
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
	if d := got.GetFrame().GetDelta(); len(d.GetPlayers()) != 1 || len(d.GetMobs()) != 1 || len(d.GetDrops()) != 1 || len(d.GetRemovedPlayers()) != 1 ||
		len(d.GetRemovedMobs()) != 1 || len(d.GetRemovedDrops()) != 1 {
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
	hat := &model.SlotItem{ID: kernel.NewItemID(), Template: "leather_cap", Quantity: 1, Stats: &progression.ItemStats{Defense: 3, MaxHP: 30}}
	if !s.Send(model.InventoryChanged{Tick: 4, Revision: 7, Slots: []model.Slot{{Tab: "equip", Slot: 0}}, Equipment: []model.EquipSlot{{Slot: "hat", Item: hat}, {Slot: "weapon"}}}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 4, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
		Revision: 7,
		Slots:    []*worldv1.Slot{{Tab: "equip", Slot: 0}},
		Equipment: []*worldv1.EquipSlot{
			{EquipSlot: "hat", Item: &worldv1.Item{Id: hat.ID.String(), TemplateId: "leather_cap", Quantity: 1, Stats: &worldv1.ItemStats{Defense: 3, MaxHp: 30}}},
			{EquipSlot: "weapon"},
		},
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestStatsChangedCarriesTheSheet(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	sheet := progression.Sheet{
		Primary: gamedata.Stats{STR: 16, VIT: 10}, PrimaryWorn: gamedata.Stats{STR: 4},
		Derived:     progression.Derived{MaxHP: 234, MaxMP: 28, AttackPower: 33.5, Accuracy: 21, Evasion: 9, Defense: 18, CritRate: 0.06, CritDamage: 0.525},
		DerivedWorn: progression.Derived{MaxHP: 30, AttackPower: 12.5, Defense: 3},
	}
	if !s.Send(model.StatsChanged{Tick: 5, Sheet: sheet, HP: 200, MaxHP: 234, MP: 20, MaxMP: 28}) {
		t.Fatal("the stream refused the change")
	}
	want := &worldv1.DeliverResponse{Tick: 5, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
		Sheet: &worldv1.StatSheet{
			Primary: &worldv1.Stats{Strength: 16, Vitality: 10}, PrimaryWorn: &worldv1.Stats{Strength: 4},
			Derived:     &worldv1.DerivedStats{AttackPower: 33.5, Defense: 18, Accuracy: 21, Evasion: 9, CritRate: 0.06, CritDamage: 0.525, MaxHp: 234, MaxMp: 28},
			DerivedWorn: &worldv1.DerivedStats{AttackPower: 12.5, Defense: 3, MaxHp: 30},
		},
		Hp: 200, MaxHp: 234, Mp: 20, MaxMp: 28,
	}}}
	if got := <-sub.C(); !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
