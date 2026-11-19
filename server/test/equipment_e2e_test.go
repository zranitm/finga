package e2e_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/progression"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// capsOnly is a copy of the game data where a green slime dies to one hit and drops a leather cap every kill, and nothing else.
func capsOnly(t *testing.T) string {
	t.Helper()
	return e2etest.GameData(t, append(e2etest.QuickSlimes(), slimesDrop(oneCap), e2etest.LootRules(1, "60s"))...) // every kill a pouch, so a hunt takes a shot or two
}

func TestEquipment(t *testing.T) {
	t.Parallel()
	t.Run("E1/a-new-character-wears-its-starter-weapon-from-the-first-welcome", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "sam", "Sam", "beginner")
		_, w := s.Hello(t, token, id)
		if len(w.Equipment) != 1 || w.Equipment[0].GetEquipSlot() != "weapon" || w.Equipment[0].GetItem().GetTemplate() != "wooden_club" ||
			w.Equipment[0].GetItem().GetStats().GetPhysicalAttack() != 8 || w.Equipment[0].GetItem().GetTab() != "" {
			t.Fatalf("equipment %+v; want the wooden club in the weapon slot at its lowest roll", w.Equipment)
		}
		if len(w.Inventory) != 1 || w.Inventory[0].GetTemplate() != "red_potion" {
			t.Fatalf("inventory %+v; want the potions alone", w.Inventory)
		}
		c := w.GetCharacter()
		if sheet := c.GetSheet(); sheet.GetDerivedWorn().GetPhysicalAttack() < 8 || sheet.GetDerived().GetPhysicalAttack() <= sheet.GetDerivedWorn().GetPhysicalAttack() ||
			sheet.GetDerived().GetMaxHp() != c.GetMaxHp() {
			t.Fatalf("sheet %+v of %+v; want the club's attack in the totals and in the equipment's share, and the maxima combat uses", sheet, c)
		}
		s.CheckInvariants(t)
	})

	t.Run("E2/on-a-full-equip-tab-a-swap-works-and-a-cap-taken-stays-in-its-pouch", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, capsOnly(t))
		token, id := s.CreateCharacter(t, "tess", "Tess", "bowman")
		s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
		// The Equip tab's 24 slots: a better bow in the first, caps in the rest.
		better := s.Give(t, id, "short_bow", &progression.ItemStats{PhysicalAttack: 11})
		for range 23 {
			s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		l, w := s.Hello(t, token, id)
		starter := w.Equipment[0].GetItem()
		if len(w.Inventory) != 25 || starter.GetTemplate() != "wooden_club" {
			t.Fatalf("welcome with %d items and %+v worn; want the full Equip tab, the potions, and the starter club worn", len(w.Inventory), w.Equipment)
		}
		before := w.GetCharacter().GetSheet().GetDerivedWorn().GetPhysicalAttack()

		l.Send(wsconn.Message{ReqID: "e-1", Data: &clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: better}})
		got := l.Gather("Response", "InventoryChanged", "StatsChanged")
		if r := got["Response"]; r.ReqID != "e-1" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("swap answered %+v", r)
		}
		ic := got["InventoryChanged"].Data.(*clientv1.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].GetTab() != "equip" || ic.Slots[0].GetSlot() != 0 || ic.Slots[0].GetItem().GetItemId() != starter.GetItemId() ||
			len(ic.Equipment) != 1 || ic.Equipment[0].GetEquipSlot() != "weapon" || ic.Equipment[0].GetItem().GetItemId() != better || ic.Revision <= w.InventoryRevision {
			t.Fatalf("swap changed %+v; want the starter club in the slot the better bow left, the bow worn", ic)
		}
		if sc := got["StatsChanged"].Data.(*clientv1.StatsChanged); sc.GetSheet().GetDerivedWorn().GetPhysicalAttack() <= before {
			t.Fatalf("stats after the swap %+v; want more than the %v attack power the starter club gave", sc, before)
		}

		if r := l.Command(t, "u-1", &clientv1.Unequip{IdempotencyKey: e2etest.Key(), EquipSlot: "weapon"}); r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("an unequip into the full tab answered %+v, want EQUIP_TAB_FULL", r)
		}

		// A slime leaves a cap in a pouch; the full tab has no slot for it, so a take leaves it there.
		pouch, ok := l.Hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap fell in 30 s of hunting")
		}
		if r := l.GoTake(t, pouch); !r.GetOk() || !slices.Equal(e2etest.Codes(r), []int32{code("EQUIP_TAB_FULL")}) {
			t.Fatalf("taking the cap answered %+v, want it refused for the full Equip tab", r)
		}
		l.Pump(time.Second, nil)
		if _, still := l.Pouches[pouch.GetId()]; !still || l.Inventory != 1 || len(l.Loots) != 0 {
			t.Fatalf("pouches %v, %d InventoryChanged, loot %v; want the cap still in its pouch and nothing taken", l.Pouches, l.Inventory, l.Loots)
		}
		var tab, caps int
		if err := s.DB.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE tab = 'equip'), count(*) FILTER (WHERE template_id = 'leather_cap')
			FROM items WHERE owner_id = $1 AND location IN ('inventory', 'equipped')`, id).Scan(&tab, &caps); err != nil {
			t.Fatal(err)
		}
		if worn := s.Worn(t, id); tab != 24 || caps != 23 || len(worn) != 1 || worn["weapon"] != better {
			t.Fatalf("%d items in the Equip tab, %d caps, %v worn; want 24, the 23 added, and the better bow", tab, caps, worn)
		}
		s.CheckInvariants(t)
	})

	t.Run("E3/an-item-the-character-cannot-wear-is-refused-with-the-reason-and-nothing-moves", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "uma", "Uma", "warrior")
		bow := s.Give(t, id, "short_bow", &progression.ItemStats{PhysicalAttack: 9})
		iron := s.Give(t, id, "iron_sword", &progression.ItemStats{PhysicalAttack: 16})
		leatherCap := s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		l, w := s.Hello(t, token, id)
		sword := w.Equipment[0].GetItem().GetItemId()
		var potion string
		for _, it := range w.Inventory {
			if it.GetTemplate() == "red_potion" {
				potion = it.GetItemId()
			}
		}
		for k, tt := range []struct {
			msg  proto.Message
			code string
		}{
			{&clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: bow}, "WRONG_CLASS"},
			{&clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: iron}, "LEVEL_TOO_LOW"},
			{&clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: potion}, "NOT_EQUIPMENT"},
			{&clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: leatherCap, EquipSlot: "weapon"}, "WRONG_EQUIP_SLOT"},
			{&clientv1.Unequip{IdempotencyKey: e2etest.Key(), EquipSlot: "hat"}, "NOTHING_WORN"},
			{&clientv1.InventoryMove{IdempotencyKey: e2etest.Key(), ItemId: sword, Tab: "equip", Slot: 9}, "ITEM_WORN"},
		} {
			time.Sleep(250 * time.Millisecond) // under the inventory limit of 5 a second
			if r := l.Command(t, fmt.Sprintf("r-%d", k), tt.msg); r.GetOk() || r.GetCode() != code(tt.code) {
				t.Fatalf("%T answered %+v, want %s", tt.msg, r, tt.code)
			}
		}
		l.Pump(500*time.Millisecond, nil)
		if worn := s.Worn(t, id); l.Inventory != 0 || len(worn) != 1 || worn["weapon"] != sword {
			t.Fatalf("%d InventoryChanged, %v worn; want nothing moved", l.Inventory, worn)
		}
		s.CheckInvariants(t)
	})

	t.Run("E4/what-is-worn-stays-through-a-map-change-and-logging-back-in", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "vic", "Vic", "warrior")
		leatherCap := s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 4})
		l, _ := s.Hello(t, token, id)
		l.Send(wsconn.Message{ReqID: "e-1", Data: &clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: leatherCap}})
		got := l.Gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("equip answered %+v", got["Response"])
		}
		sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet()
		if sheet.GetDerivedWorn().GetPhysicalDefense() != 4 {
			t.Fatalf("sheet %+v; want the cap's 4 defense as the equipment's share", sheet)
		}
		l.Hop(t, e2etest.TownPortal, "forest", e2etest.ForestFromTown)
		_, w := s.Welcome(t, "vic", id)
		wearing := map[string]*clientv1.ItemView{}
		for _, e := range w.Equipment {
			wearing[e.GetEquipSlot()] = e.GetItem()
		}
		if w.GetMapId() != "forest" || len(wearing) != 2 || wearing["weapon"].GetTemplate() != "wooden_club" ||
			wearing["hat"].GetItemId() != leatherCap || wearing["hat"].GetStats().GetPhysicalDefense() != 4 {
			t.Fatalf("welcomed on %s wearing %+v; want the forest, the club and the cap", w.GetMapId(), w.Equipment)
		}
		if !proto.Equal(w.GetCharacter().GetSheet(), sheet) {
			t.Fatalf("welcome sheet %+v, StatsChanged said %+v", w.GetCharacter().GetSheet(), sheet)
		}
		for _, it := range w.Inventory {
			if it.GetItemId() == leatherCap {
				t.Fatalf("the worn cap is in the inventory too: %+v", w.Inventory)
			}
		}
	})

	t.Run("E5/a-dead-character-cannot-change-equipment-and-keeps-it-through-the-respawn", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "wes", "Wes", "warrior")
		s.Place(t, id, "forest", e2etest.SlimeField, 1, 0)
		l, w := s.Hello(t, token, id)
		sword := w.Equipment[0].GetItem().GetItemId()
		l.DieAmongSlimes(t)
		// Respawn waits three seconds; the unequip arrives well within them.
		if r := l.Command(t, "u-1", &clientv1.Unequip{IdempotencyKey: e2etest.Key(), EquipSlot: "weapon"}); r.GetCode() != code("CHARACTER_DEAD") {
			t.Fatalf("an unequip while dead answered %+v, want CHARACTER_DEAD", r)
		}
		if r := l.Expect("Respawned").Data.(*clientv1.Respawned); r.GetHp() != w.GetCharacter().GetMaxHp()/2 {
			t.Fatalf("respawned with %d HP, want half the %d maximum with equipment", r.GetHp(), w.GetCharacter().GetMaxHp())
		}
		if worn := s.Worn(t, id); len(worn) != 1 || worn["weapon"] != sword {
			t.Fatalf("worn after the respawn: %v, want the sword", worn)
		}
		_, again := s.Welcome(t, "wes", id)
		if len(again.Equipment) != 1 || again.Equipment[0].GetItem().GetItemId() != sword {
			t.Fatalf("welcomed wearing %+v, want the sword", again.Equipment)
		}
	})

	t.Run("E6/a-swap-during-a-restart-loses-and-duplicates-nothing-and-its-retry-applies", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "xan", "Xan", "warrior")
		better := s.Give(t, id, "wooden_sword", &progression.ItemStats{PhysicalAttack: 12})
		l, w := s.Hello(t, token, id)
		starter := w.Equipment[0].GetItem().GetItemId()
		swap := &clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: better}
		l.Send(wsconn.Message{ReqID: "e-1", Data: swap})
		s.Restart()
		l, _ = s.Hello(t, l.Token, id) // the client reconnects with its session
		// Whether the swap committed before the stop or not, the item module holds both weapons and wears one.
		var weapons int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM items WHERE owner_id = $1 AND template_id IN ('wooden_club', 'wooden_sword') AND location IN ('inventory', 'equipped')`, id).Scan(&weapons); err != nil {
			t.Fatal(err)
		}
		if worn := s.Worn(t, id); weapons != 2 || len(worn) != 1 || (worn["weapon"] != starter && worn["weapon"] != better) {
			t.Fatalf("%d weapons, %v worn; want the club and the sword and one of them worn", weapons, worn)
		}
		s.CheckInvariants(t)

		// The retry with the same key gets the stored reply if the swap committed, or makes it now.
		if r := l.Command(t, "e-2", swap); !r.GetOk() {
			t.Fatalf("the retry answered %+v", r)
		}
		if worn := s.Worn(t, id); len(worn) != 1 || worn["weapon"] != better {
			t.Fatalf("worn after the retry: %v, want the better sword", worn)
		}
		_, again := s.Welcome(t, "xan", id)
		if len(again.Equipment) != 1 || again.Equipment[0].GetItem().GetItemId() != better || again.GetCharacter().GetSheet().GetDerivedWorn().GetPhysicalAttack() <= w.GetCharacter().GetSheet().GetDerivedWorn().GetPhysicalAttack() {
			t.Fatalf("welcomed wearing %+v with sheet %+v; want the better sword and its attack", again.Equipment, again.GetCharacter().GetSheet())
		}
		s.CheckInvariants(t)
	})

	t.Run("E7/an-item-below-its-stat-requirement-is-refused-until-points-meet-it-and-adds-nothing-after-a-reset", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "wade", "Wade", "warrior")
		// Level 8 with its 35 points unspent.
		if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET level = 8, unspent_points = 35 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		iron := s.Give(t, id, "iron_sword", &progression.ItemStats{PhysicalAttack: 18}) // needs STR 20
		l, _ := s.Hello(t, token, id)
		if r := l.Command(t, "e-1", &clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: iron}); r.GetOk() || r.GetCode() != code("STR_TOO_LOW") {
			t.Fatalf("equipping with STR 7 answered %+v, want STR_TOO_LOW", r)
		}
		l.Send(wsconn.Message{ReqID: "s-1", Data: &clientv1.SpendPoints{ExpectedUnspent: 35, Points: &clientv1.Stats{Str: 13}}})
		if r := l.Gather("Response", "StatsChanged")["Response"]; !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("spend answered %+v", r)
		}
		time.Sleep(250 * time.Millisecond) // under the inventory limit
		l.Send(wsconn.Message{ReqID: "e-2", Data: &clientv1.Equip{IdempotencyKey: e2etest.Key(), ItemId: iron}})
		got := l.Gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("equipping with STR 20 answered %+v", got["Response"])
		}
		if sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet(); sheet.GetDerivedWorn().GetPhysicalAttack() != 18 {
			t.Fatalf("sheet %+v; want the sword's 18 attack", sheet)
		}
		l.Send(wsconn.Message{ReqID: "r-1", Data: &clientv1.ResetPoints{ExpectedUnspent: 22}})
		got = l.Gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("reset answered %+v", got["Response"])
		}
		if sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet(); sheet.GetPrimary().GetStr() != 7 || sheet.GetDerivedWorn().GetPhysicalAttack() != 0 {
			t.Fatalf("sheet after the reset %+v; want STR 7 and the sword adding nothing", sheet)
		}
		if worn := s.Worn(t, id); worn["weapon"] != iron {
			t.Fatalf("worn %v; want the sword still worn", worn)
		}
		s.CheckInvariants(t)
	})
}
