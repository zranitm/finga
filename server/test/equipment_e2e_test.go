package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/item"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// capsOnly is a copy of the game data where a green slime dies to one hit and drops a leather cap every kill, and nothing else.
func capsOnly(t *testing.T) string {
	t.Helper()
	return gameData(t, append(quickSlimes(), slimesDrop(oneCap))...)
}

// actor is the character as the item module names who acts.
func (s *stack) actor(t *testing.T, id string) item.Actor {
	t.Helper()
	var account string
	if err := s.db.QueryRow(context.Background(), `SELECT account_id FROM characters WHERE id = $1`, id).Scan(&account); err != nil {
		t.Fatal(err)
	}
	return item.Actor{Account: mustID(t, accountID, account), Character: mustID(t, kernel.ParseCharacterID, id)}
}

// accountID is s as an account's ID, as the database holds it.
func accountID(s string) (kernel.AccountID, error) {
	var id kernel.AccountID
	err := id.UnmarshalText([]byte(s))
	return id, err
}

// mustID is s parsed by parse.
func mustID[ID any](t *testing.T, parse func(string) (ID, error), s string) ID {
	t.Helper()
	id, err := parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// give adds an item through the item module, as a drop would, and returns its ID.
func (s *stack) give(t *testing.T, id, template string, stats *progression.ItemStats) string {
	t.Helper()
	key := uuidv7.New()
	res, err := s.items.Add(context.Background(), s.actor(t, id), item.AddRequest{Key: key.String(), TemplateID: template, Quantity: 1, Cause: item.Cause{Type: item.CauseDrop, ID: key}, Stats: stats})
	if err != nil {
		t.Fatal(err)
	}
	return res.Changed[len(res.Changed)-1].Item.ID.String()
}

// worn is what the database says the character wears, item ID by equip slot.
func (s *stack) worn(t *testing.T, id string) map[string]string {
	t.Helper()
	rows, err := s.db.Query(context.Background(), `SELECT equip_slot, id::text FROM items WHERE owner_id = $1 AND location = 'equipped'`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var slot, item string
		if err := rows.Scan(&slot, &item); err != nil {
			t.Fatal(err)
		}
		out[slot] = item
	}
	return out
}

// hello joins over a new connection and returns the Welcome with a looter on the Snapshot.
func (s *stack) hello(t *testing.T, token, id string) (*looter, *clientv1.Welcome) {
	t.Helper()
	p := s.connect(t, id)
	w, snap := p.hello(token)
	return newLooter(p, snap), w
}

// gather reads until one message of each type has arrived, in any order: a Response and what the world's stream sends race.
func (l *looter) gather(types ...string) map[string]wsconn.Message {
	l.t.Helper()
	got := map[string]wsconn.Message{}
	l.pump(5*time.Second, func(m wsconn.Message) bool {
		for _, typ := range types {
			if _, seen := got[typ]; m.Type == typ && !seen {
				got[typ] = m
			}
		}
		return len(got) == len(types)
	})
	if len(got) != len(types) {
		l.t.Fatalf("got %v of %v within 5 s", got, types)
	}
	return got
}

func key() string { return "k-" + uuidv7.NewString() }

func TestEquipment(t *testing.T) {
	t.Parallel()
	t.Run("E1/a-new-character-wears-its-starter-weapon-from-the-first-welcome", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "sam", "Sam", "beginner")
		_, w := s.hello(t, token, id)
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
		s.checkInvariants(t)
	})

	t.Run("E2/on-a-full-equip-tab-a-swap-works-and-a-drop-waits-on-the-ground", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, capsOnly(t))
		token, id := s.createCharacter(t, "tess", "Tess", "bowman")
		s.place(t, id, "forest", 380, 608, 134, 0)
		// The Equip tab's 24 slots: a better bow in the first, caps in the rest.
		better := s.give(t, id, "short_bow", &progression.ItemStats{PhysicalAttack: 11})
		for range 23 {
			s.give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		l, w := s.hello(t, token, id)
		starter := w.Equipment[0].GetItem()
		if len(w.Inventory) != 25 || starter.GetTemplate() != "wooden_club" {
			t.Fatalf("welcome with %d items and %+v worn; want the full Equip tab, the potions, and the starter club worn", len(w.Inventory), w.Equipment)
		}
		before := w.GetCharacter().GetSheet().GetDerivedWorn().GetPhysicalAttack()

		l.send(wsconn.Message{ReqID: "e-1", Data: &clientv1.Equip{IdempotencyKey: key(), ItemId: better}})
		got := l.gather("Response", "InventoryChanged", "StatsChanged")
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

		if r := l.command(t, "u-1", &clientv1.Unequip{IdempotencyKey: key(), EquipSlot: "weapon"}); r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("an unequip into the full tab answered %+v, want EQUIP_TAB_FULL", r)
		}

		// A slime drops a cap; the full tab has no slot for it, so it waits on the ground.
		l.face(true)
		drop, ok := l.hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap dropped in 30 s of hunting")
		}
		// No key: past its collect delay and a room check, the cap is still on the ground, waiting, and nothing reached the inventory.
		l.pump(4*time.Second, nil)
		if _, still := l.drops[drop.GetId()]; !still || l.inventory != 1 {
			t.Fatalf("the cap left the ground or something reached the inventory: drops %v, %d InventoryChanged", l.drops, l.inventory)
		}
		var waiting int
		for _, lt := range l.loots {
			if lt.GetDropId() == drop.GetId() && lt.GetKind() == clientv1.LootKind_LOOT_KIND_WAITING {
				waiting++
			}
		}
		if waiting != 1 {
			t.Fatalf("loot %v; want the cap told waiting, once", l.loots)
		}
		var tab, caps int
		if err := s.db.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE tab = 'equip'), count(*) FILTER (WHERE template_id = 'leather_cap')
			FROM items WHERE owner_id = $1 AND location IN ('inventory', 'equipped')`, id).Scan(&tab, &caps); err != nil {
			t.Fatal(err)
		}
		if worn := s.worn(t, id); tab != 24 || caps != 23 || len(worn) != 1 || worn["weapon"] != better {
			t.Fatalf("%d items in the Equip tab, %d caps, %v worn; want 24, the 23 added, and the better bow", tab, caps, worn)
		}
		s.checkInvariants(t)
	})

	t.Run("E3/an-item-the-character-cannot-wear-is-refused-with-the-reason-and-nothing-moves", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "uma", "Uma", "warrior")
		bow := s.give(t, id, "short_bow", &progression.ItemStats{PhysicalAttack: 9})
		iron := s.give(t, id, "iron_sword", &progression.ItemStats{PhysicalAttack: 16})
		leatherCap := s.give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		l, w := s.hello(t, token, id)
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
			{&clientv1.Equip{IdempotencyKey: key(), ItemId: bow}, "WRONG_CLASS"},
			{&clientv1.Equip{IdempotencyKey: key(), ItemId: iron}, "LEVEL_TOO_LOW"},
			{&clientv1.Equip{IdempotencyKey: key(), ItemId: potion}, "NOT_EQUIPMENT"},
			{&clientv1.Equip{IdempotencyKey: key(), ItemId: leatherCap, EquipSlot: "weapon"}, "WRONG_EQUIP_SLOT"},
			{&clientv1.Unequip{IdempotencyKey: key(), EquipSlot: "hat"}, "NOTHING_WORN"},
			{&clientv1.InventoryMove{IdempotencyKey: key(), ItemId: sword, Tab: "equip", Slot: 9}, "ITEM_WORN"},
		} {
			time.Sleep(250 * time.Millisecond) // under the inventory limit of 5 a second
			if r := l.command(t, fmt.Sprintf("r-%d", k), tt.msg); r.GetOk() || r.GetCode() != code(tt.code) {
				t.Fatalf("%T answered %+v, want %s", tt.msg, r, tt.code)
			}
		}
		l.pump(500*time.Millisecond, nil)
		if worn := s.worn(t, id); l.inventory != 0 || len(worn) != 1 || worn["weapon"] != sword {
			t.Fatalf("%d InventoryChanged, %v worn; want nothing moved", l.inventory, worn)
		}
		s.checkInvariants(t)
	})

	t.Run("E4/what-is-worn-stays-through-a-map-change-and-logging-back-in", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "vic", "Vic", "warrior")
		leatherCap := s.give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 4})
		l, _ := s.hello(t, token, id)
		l.send(wsconn.Message{ReqID: "e-1", Data: &clientv1.Equip{IdempotencyKey: key(), ItemId: leatherCap}})
		got := l.gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("equip answered %+v", got["Response"])
		}
		sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet()
		if sheet.GetDerivedWorn().GetPhysicalDefense() != 4 {
			t.Fatalf("sheet %+v; want the cap's 4 defense as the equipment's share", sheet)
		}
		l.hop(t, 1216, "forest", 64)
		_, w := s.welcome(t, "vic", id)
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
		token, id := s.createCharacter(t, "wes", "Wes", "warrior")
		s.place(t, id, "forest", 380, 608, 1, 0)
		l, w := s.hello(t, token, id)
		sword := w.Equipment[0].GetItem().GetItemId()
		l.dieAmongSlimes(t)
		// Respawn waits three seconds; the unequip arrives well within them.
		if r := l.command(t, "u-1", &clientv1.Unequip{IdempotencyKey: key(), EquipSlot: "weapon"}); r.GetCode() != code("CHARACTER_DEAD") {
			t.Fatalf("an unequip while dead answered %+v, want CHARACTER_DEAD", r)
		}
		if r := l.expect("Respawned").Data.(*clientv1.Respawned); r.GetHp() != w.GetCharacter().GetMaxHp()/2 {
			t.Fatalf("respawned with %d HP, want half the %d maximum with equipment", r.GetHp(), w.GetCharacter().GetMaxHp())
		}
		if worn := s.worn(t, id); len(worn) != 1 || worn["weapon"] != sword {
			t.Fatalf("worn after the respawn: %v, want the sword", worn)
		}
		_, again := s.welcome(t, "wes", id)
		if len(again.Equipment) != 1 || again.Equipment[0].GetItem().GetItemId() != sword {
			t.Fatalf("welcomed wearing %+v, want the sword", again.Equipment)
		}
	})

	t.Run("E6/a-swap-during-a-restart-loses-and-duplicates-nothing-and-its-retry-applies", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "xan", "Xan", "warrior")
		better := s.give(t, id, "wooden_sword", &progression.ItemStats{PhysicalAttack: 12})
		l, w := s.hello(t, token, id)
		starter := w.Equipment[0].GetItem().GetItemId()
		swap := &clientv1.Equip{IdempotencyKey: key(), ItemId: better}
		l.send(wsconn.Message{ReqID: "e-1", Data: swap})
		s.restart()
		l, _ = s.hello(t, l.token, id) // the client reconnects with its session
		// Whether the swap committed before the stop or not, the item module holds both weapons and wears one.
		var weapons int
		if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM items WHERE owner_id = $1 AND template_id IN ('wooden_club', 'wooden_sword') AND location IN ('inventory', 'equipped')`, id).Scan(&weapons); err != nil {
			t.Fatal(err)
		}
		if worn := s.worn(t, id); weapons != 2 || len(worn) != 1 || (worn["weapon"] != starter && worn["weapon"] != better) {
			t.Fatalf("%d weapons, %v worn; want the club and the sword and one of them worn", weapons, worn)
		}
		s.checkInvariants(t)

		// The retry with the same key gets the stored reply if the swap committed, or makes it now.
		if r := l.command(t, "e-2", swap); !r.GetOk() {
			t.Fatalf("the retry answered %+v", r)
		}
		if worn := s.worn(t, id); len(worn) != 1 || worn["weapon"] != better {
			t.Fatalf("worn after the retry: %v, want the better sword", worn)
		}
		_, again := s.welcome(t, "xan", id)
		if len(again.Equipment) != 1 || again.Equipment[0].GetItem().GetItemId() != better || again.GetCharacter().GetSheet().GetDerivedWorn().GetPhysicalAttack() <= w.GetCharacter().GetSheet().GetDerivedWorn().GetPhysicalAttack() {
			t.Fatalf("welcomed wearing %+v with sheet %+v; want the better sword and its attack", again.Equipment, again.GetCharacter().GetSheet())
		}
		s.checkInvariants(t)
	})

	t.Run("E7/an-item-below-its-stat-requirement-is-refused-until-points-meet-it-and-adds-nothing-after-a-reset", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "wade", "Wade", "warrior")
		// Level 8 with its 35 points unspent.
		if _, err := s.db.Exec(context.Background(), `UPDATE characters SET level = 8, unspent_points = 35 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		iron := s.give(t, id, "iron_sword", &progression.ItemStats{PhysicalAttack: 18}) // needs STR 20
		l, _ := s.hello(t, token, id)
		if r := l.command(t, "e-1", &clientv1.Equip{IdempotencyKey: key(), ItemId: iron}); r.GetOk() || r.GetCode() != code("STR_TOO_LOW") {
			t.Fatalf("equipping with STR 7 answered %+v, want STR_TOO_LOW", r)
		}
		l.send(wsconn.Message{ReqID: "s-1", Data: &clientv1.SpendPoints{ExpectedUnspent: 35, Points: &clientv1.Stats{Str: 13}}})
		if r := l.gather("Response", "StatsChanged")["Response"]; !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("spend answered %+v", r)
		}
		time.Sleep(250 * time.Millisecond) // under the inventory limit
		l.send(wsconn.Message{ReqID: "e-2", Data: &clientv1.Equip{IdempotencyKey: key(), ItemId: iron}})
		got := l.gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("equipping with STR 20 answered %+v", got["Response"])
		}
		if sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet(); sheet.GetDerivedWorn().GetPhysicalAttack() != 18 {
			t.Fatalf("sheet %+v; want the sword's 18 attack", sheet)
		}
		l.send(wsconn.Message{ReqID: "r-1", Data: &clientv1.ResetPoints{ExpectedUnspent: 22}})
		got = l.gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("reset answered %+v", got["Response"])
		}
		if sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet(); sheet.GetPrimary().GetStr() != 7 || sheet.GetDerivedWorn().GetPhysicalAttack() != 0 {
			t.Fatalf("sheet after the reset %+v; want STR 7 and the sword adding nothing", sheet)
		}
		if worn := s.worn(t, id); worn["weapon"] != iron {
			t.Fatalf("worn %v; want the sword still worn", worn)
		}
		s.checkInvariants(t)
	})
}
