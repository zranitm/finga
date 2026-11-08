package e2e_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/protocol"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// capsOnly is a copy of the game data where a green slime drops a leather cap every kill, and nothing else.
func capsOnly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../data")); err != nil {
		t.Fatal(err)
	}
	mobs := filepath.Join(dir, "mobs.yaml")
	b, err := os.ReadFile(mobs)
	if err != nil {
		t.Fatal(err)
	}
	const slime = "      - {item: slime_gel, chance: 0.6, min: 1, max: 2}\n      - {item: red_potion, chance: 0.15, min: 1, max: 1}\n"
	if !bytes.Contains(b, []byte(slime)) {
		t.Fatal("mobs.yaml no longer lists the green slime's drops as this test rewrites them")
	}
	b = bytes.Replace(b, []byte(slime), []byte("      - {item: leather_cap, chance: 1, min: 1, max: 1}\n"), 1)
	if err := os.WriteFile(mobs, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// hello joins over a new connection and returns the Welcome with a looter on the Snapshot.
func (s *stack) hello(t *testing.T, token, id string) (*looter, *clientv1.Welcome) {
	t.Helper()
	p := s.connect(t, id)
	p.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: id}})
	w := p.expect("Welcome").Data.(*clientv1.Welcome)
	p.token = w.Token
	return newLooter(p, p.expect("Snapshot").Data.(*clientv1.Snapshot)), w
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

func TestEquipment(t *testing.T) {
	t.Run("E1/a-new-character-wears-its-starter-weapon-from-the-first-welcome", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "sam", "Sam", "warrior")
		_, w := s.hello(t, token, id)
		if len(w.Equipment) != 1 || w.Equipment[0].GetEquipSlot() != "weapon" || w.Equipment[0].GetItem().GetTemplate() != "wooden_sword" ||
			w.Equipment[0].GetItem().GetStats().GetAttack() != 10 || w.Equipment[0].GetItem().GetTab() != "" {
			t.Fatalf("equipment %+v; want the wooden sword in the weapon slot at its lowest roll", w.Equipment)
		}
		if len(w.Inventory) != 1 || w.Inventory[0].GetTemplate() != "red_potion" {
			t.Fatalf("inventory %+v; want the potions alone", w.Inventory)
		}
		if sheet := w.GetCharacter().GetSheet(); sheet.GetDerivedWorn().GetAttackPower() < 10 || sheet.GetDerived().GetAttackPower() <= sheet.GetDerivedWorn().GetAttackPower() {
			t.Fatalf("sheet %+v; want the sword's attack in the totals and in the equipment's share", sheet)
		}
		s.checkInvariants(t)
	})

	t.Run("E2/on-a-full-equip-tab-a-swap-works-and-a-pickup-is-refused-leaving-the-drop", func(t *testing.T) {
		s := newStackOver(t, capsOnly(t))
		token, id := s.createCharacter(t, "tess", "Tess", "warrior")
		s.place(t, id, "forest", 380, 608, 204, 0)
		var account string
		if err := s.db.QueryRow(context.Background(), `SELECT account_id FROM characters WHERE id = $1`, id).Scan(&account); err != nil {
			t.Fatal(err)
		}
		// The Equip tab's 24 slots: a better sword in the first, caps in the rest.
		actor := &itemv1.Actor{AccountId: account, CharacterId: id}
		add := func(template string, stats *itemv1.ItemStats) string {
			res, err := s.items.Add(context.Background(), &itemv1.AddRequest{Actor: actor, IdempotencyKey: uuidv7.NewString(), TemplateId: template, Quantity: 1,
				Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: uuidv7.NewString()}, Stats: stats})
			if err != nil {
				t.Fatal(err)
			}
			return res.GetChanged()[0].GetItem().GetId()
		}
		better := add("wooden_sword", &itemv1.ItemStats{Attack: 12})
		for range 23 {
			add("leather_cap", &itemv1.ItemStats{Defense: 3})
		}
		l, w := s.hello(t, token, id)
		starter := w.Equipment[0].GetItem()
		if len(w.Inventory) != 25 || starter.GetTemplate() != "wooden_sword" {
			t.Fatalf("welcome with %d items and %+v worn; want the full Equip tab, the potions, and the starter sword worn", len(w.Inventory), w.Equipment)
		}
		before := w.GetCharacter().GetSheet().GetDerivedWorn().GetAttackPower()

		l.send(wsconn.Message{ReqID: "e-1", Data: &clientv1.Equip{IdempotencyKey: "k-" + uuidv7.NewString(), ItemId: better}})
		got := l.gather("Response", "InventoryChanged", "StatsChanged")
		if r := got["Response"]; r.ReqID != "e-1" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("swap answered %+v", r)
		}
		ic := got["InventoryChanged"].Data.(*clientv1.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].GetTab() != "equip" || ic.Slots[0].GetSlot() != 0 || ic.Slots[0].GetItem().GetItemId() != starter.GetItemId() ||
			len(ic.Equipment) != 1 || ic.Equipment[0].GetEquipSlot() != "weapon" || ic.Equipment[0].GetItem().GetItemId() != better || ic.Revision <= w.InventoryRevision {
			t.Fatalf("swap changed %+v; want the starter sword in the slot the better one left, the better one worn", ic)
		}
		if sc := got["StatsChanged"].Data.(*clientv1.StatsChanged); sc.GetSheet().GetDerivedWorn().GetAttackPower() <= before {
			t.Fatalf("stats after the swap %+v; want more than the %v attack power the starter sword gave", sc, before)
		}

		l.send(wsconn.Message{ReqID: "u-1", Data: &clientv1.Unequip{IdempotencyKey: "k-" + uuidv7.NewString(), EquipSlot: "weapon"}})
		if r := l.gather("Response")["Response"]; r.ReqID != "u-1" || r.Data.(*clientv1.Response).GetCode() != "inventory_full" {
			t.Fatalf("an unequip into the full tab answered %+v, want inventory_full", r)
		}

		// A slime drops a cap; the full tab has no slot for it, so it stays on the ground.
		l.face(true)
		drop, ok := l.hunt(90*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap dropped in 90 s of hunting")
		}
		refused := false
		for attempt := 0; attempt < 10 && !refused; attempt++ {
			if !l.walkTo(drop.X, 15*time.Second) {
				t.Fatalf("could not reach the cap at %.0f (at %.0f)", drop.X, l.x)
			}
			if err := l.pickUp(); err != nil {
				t.Fatal(err)
			}
			refused = l.pump(3*time.Second, func(m wsconn.Message) bool {
				r, isResponse := m.Data.(*clientv1.Response)
				return isResponse && r.GetCode() == "inventory_full"
			})
		}
		if !refused {
			t.Fatalf("no inventory_full after PickUp (failures %+v, at %.0f, drop %+v)", l.failures, l.x, drop)
		}
		l.pump(time.Second, nil)
		if _, still := l.drops[drop.GetId()]; !still || l.inventory != 1 {
			t.Fatalf("the cap left the ground or something reached the inventory: drops %v, %d InventoryChanged", l.drops, l.inventory)
		}
		var tab, caps int
		var worn string
		if err := s.db.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE tab = 'equip'), count(*) FILTER (WHERE template_id = 'leather_cap'), coalesce(max(id::text) FILTER (WHERE equip_slot = 'weapon'), '')
			FROM items WHERE owner_id = $1 AND location IN ('inventory', 'equipped')`, id).Scan(&tab, &caps, &worn); err != nil {
			t.Fatal(err)
		}
		if tab != 24 || caps != 23 || worn != better {
			t.Fatalf("%d items in the Equip tab, %d caps, %s worn; want 24, the 23 added, and the better sword", tab, caps, worn)
		}
		s.checkInvariants(t)
	})
}
