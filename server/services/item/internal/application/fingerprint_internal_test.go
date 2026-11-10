package application

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/item/internal/model"
)

// A changed fingerprint refuses every retry stored before the deploy; a deliberate change bumps the version and value.
func TestFingerprintsAreStable(t *testing.T) {
	c, err := kernel.ParseCharacterID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9012")
	if err != nil {
		t.Fatal(err)
	}
	item, err := kernel.ParseItemID("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9a0b")
	if err != nil {
		t.Fatal(err)
	}
	drop := uuid.MustParse("0190a0f4-1c2d-7e3f-8a4b-5c6d7e8f9c0d")
	slot := 7
	for _, tc := range []struct {
		name string
		got  []byte
		want string
	}{
		{"add", AddInput{Key: "k-1", TemplateID: "slime_gel", Quantity: 3, Cause: model.Cause{Type: model.CauseDrop, ID: drop}}.fingerprint(c), "4b9139f2119c59ece19bb810e5be6562a12018c124918fb30695d0f444515826"},
		{"add with a roll", AddInput{Key: "k-1", TemplateID: "iron_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: drop}, Stats: &progression.ItemStats{PhysicalAttack: 17, CritRate: 5}}.fingerprint(c), "7d0437335f33ffc45e881b77150141f19e6140fbe95a066eaa793f4f7f764e66"},
		{"add with a later stat", AddInput{Key: "k-1", TemplateID: "iron_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: drop}, Stats: &progression.ItemStats{PhysicalAttack: 17, MagicAttack: 4, MoveSpeed: 20}}.fingerprint(c), "280eedd37257ce7d88a2c5856a3f947d5e74a068daea6c274aeeb0863e24efdf"},
		{"move", MoveInput{Key: "k-1", ItemID: item, Tab: gamedata.Use, Slot: 7}.fingerprint(c), "75b5e3b6161dbf0fe70935cb9e77fbb717c119ff92df45c1c9d951780f49e026"},
		{"equip", EquipInput{Key: "k-1", ItemID: item, EquipSlot: "ring_2", Level: 9, Class: kernel.Warrior}.fingerprint(c), "4d802f08e9649e4e7d82a0577e6e6b94655d4a9ab4f4dbd0ce2474cab43d8023"},
		{"unequip", UnequipInput{Key: "k-1", EquipSlot: "ring_2"}.fingerprint(c), "073208012179538dcf3df878102415c402d45d6bb8c5bb35a7a4072838584c51"},
		{"unequip to a slot", UnequipInput{Key: "k-1", EquipSlot: "ring_2", ToSlot: &slot}.fingerprint(c), "14261d0402ea762f54c4b35d003f976f5c4358e134d16bacb86463a987a78394"},
		{"discard", DiscardInput{Key: "k-1", ItemID: item, Quantity: 2}.fingerprint(c), "693b3c5bd5cd91ea7cd9157dead199a007206e0f3090fafb9ad8b11cfdaba75e"},
	} {
		if got := hex.EncodeToString(tc.got); got != tc.want {
			t.Errorf("%s fingerprint changed: retries made before this deploy would be refused\ngot  %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// The same input from another character is another request, and the key is not part of it.
func TestAFingerprintNamesTheCharacterNotTheKey(t *testing.T) {
	in := MoveInput{Key: "k-1", ItemID: kernel.NewItemID(), Tab: gamedata.Use, Slot: 7}
	c := kernel.NewCharacterID()
	if hex.EncodeToString(in.fingerprint(c)) == hex.EncodeToString(in.fingerprint(kernel.NewCharacterID())) {
		t.Fatal("two characters' moves share a fingerprint")
	}
	other := in
	other.Key = "k-2"
	if hex.EncodeToString(in.fingerprint(c)) != hex.EncodeToString(other.fingerprint(c)) {
		t.Fatal("the key changed the fingerprint")
	}
}

// A roll is part of an add: the same drop rolled otherwise is another request.
func TestAnAddsRollIsPartOfItsFingerprint(t *testing.T) {
	c := kernel.NewCharacterID()
	in := AddInput{Key: "k-1", TemplateID: "iron_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop}, Stats: &progression.ItemStats{PhysicalAttack: 17}}
	other := in
	other.Stats = &progression.ItemStats{PhysicalAttack: 18}
	none := in
	none.Stats = nil
	if hex.EncodeToString(in.fingerprint(c)) == hex.EncodeToString(other.fingerprint(c)) || hex.EncodeToString(in.fingerprint(c)) == hex.EncodeToString(none.fingerprint(c)) {
		t.Fatal("two rolls, or a roll and none, share a fingerprint")
	}
	seen := map[string]string{hex.EncodeToString(none.fingerprint(c)): "no roll"}
	stats := reflect.TypeFor[progression.ItemStats]()
	for i := range stats.NumField() {
		var roll progression.ItemStats
		reflect.ValueOf(&roll).Elem().Field(i).SetInt(1)
		one := in
		one.Stats = &roll
		fp := hex.EncodeToString(one.fingerprint(c))
		if was, ok := seen[fp]; ok {
			t.Fatalf("a roll of 1 %s shares a fingerprint with %s", stats.Field(i).Name, was)
		}
		seen[fp] = stats.Field(i).Name
	}
}

// An equip is the item and the slot: a retry after a level-up, a spend, or as another class, is the same request.
func TestAnEquipsFingerprintLeavesOutTheLevelClassAndStats(t *testing.T) {
	c, item := kernel.NewCharacterID(), kernel.NewItemID()
	in := EquipInput{Key: "k-1", ItemID: item, Level: 3, Class: kernel.Warrior, Stats: gamedata.Stats{STR: 12}}
	later := in
	later.Level, later.Class, later.Stats = 4, kernel.Bowman, gamedata.Stats{STR: 13, DEX: 5}
	elsewhere := in
	elsewhere.EquipSlot = "ring_2"
	if hex.EncodeToString(in.fingerprint(c)) != hex.EncodeToString(later.fingerprint(c)) || hex.EncodeToString(in.fingerprint(c)) == hex.EncodeToString(elsewhere.fingerprint(c)) {
		t.Fatal("the level, class or stats are part of an equip, or the slot is not")
	}
}
