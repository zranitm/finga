package e2e_test

import (
	"context"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/progression"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// slimesDrop has a green slime drop rows, in mobs.yaml's form, instead of its gel and potion.
func slimesDrop(rows string) e2etest.DataEdit {
	return e2etest.DataEdit{File: "mobs.yaml", Old: "      - {item: slime_gel, chance: 0.9, min: 1, max: 2}\n      - {item: red_potion, chance: 0.225, min: 1, max: 1}\n", New: rows}
}

const (
	oneGel = "      - {item: slime_gel, chance: 1, min: 1, max: 1}\n"
	oneCap = "      - {item: leather_cap, chance: 1, min: 1, max: 1}\n"
)

// everyKill has every kill leave a pouch, for a minute, so a hunt takes a shot or two.
var everyKill = e2etest.LootRules(1, "60s")

func TestLootAndKeep(t *testing.T) {
	t.Parallel()
	t.Run("D1-D2/a-kill-leaves-its-killer-a-pouch-nobody-else-sees-and-a-take-grants-each-item-once", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), everyKill)...))
		token, id := s.CreateCharacter(t, "liam", "Liam", "bowman")
		s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
		p, snap := s.Join(t, token, id)
		killer := e2etest.NewLooter(p, snap)
		token2, id2 := s.CreateCharacter(t, "mia", "Mia", "bowman")
		s.Place(t, id2, "forest", gamedata.Point{X: e2etest.SlimeField.X - 0.625, Z: e2etest.SlimeField.Z}, 134, 0)
		wp, wsnap := s.Join(t, token2, id2)
		watcher := e2etest.NewLooter(wp, wsnap)
		killer.Pump(300*time.Millisecond, nil)

		pouch, ok := killer.Hunt(30*time.Second, "")
		if !ok {
			t.Fatal("no pouch in 30 s of hunting")
		}
		// The killer stands still: a kill is within the bow's 13.125 m and a slime's 0.5 m of it, and its pouch lies where it died, on the ground.
		data, err := load.Dir(s.Data)
		if err != nil {
			t.Fatal(err)
		}
		forest, _ := data.Map("forest")
		if pouch.Y != forest.Geometry.Terrain.Height(pouch.X, pouch.Z) || math.Hypot(pouch.X-killer.X, pouch.Z-killer.Z) > 13.125+0.5 || pouch.GetKind() != clientv1.PouchKind_POUCH_KIND_POUCH {
			t.Fatalf("pouch %+v, the killer at (%v, %v)", pouch, killer.X, killer.Z)
		}
		for _, it := range pouch.GetItems() {
			if it.GetQuantity() < 1 || (it.GetTemplate() != "slime_gel" && it.GetTemplate() != "red_potion") {
				t.Fatalf("pouch %+v holds %+v", pouch, it)
			}
		}
		// No key, no pickup: a few seconds on, nothing reached the inventory, and the watcher never heard of the pouch.
		killer.Pump(2*time.Second, nil)
		watcher.Pump(time.Second, nil)
		if killer.Inventory != 0 || len(watcher.Seen) != 0 {
			t.Fatalf("%d InventoryChanged before a take; the watcher saw pouches %v", killer.Inventory, watcher.Seen)
		}

		r := killer.GoTake(t, pouch)
		if !r.GetOk() || slices.ContainsFunc(e2etest.Codes(r), func(c int32) bool { return c != 0 }) || len(r.GetPicks()) != len(pouch.GetItems()) {
			t.Fatalf("the take answered %+v; want each item taken", r)
		}
		if !killer.Until(5*time.Second, func() bool {
			return killer.Gone[pouch.GetId()] && killer.Inventory > 0 && len(killer.LootOf(pouch.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)) == 1
		}) {
			t.Fatalf("gone %v, %d InventoryChanged, loot %v; want the pouch emptied and its items taken (failures %+v)", killer.Gone, killer.Inventory, killer.Loots, killer.Failures)
		}
		if lt := killer.LootOf(pouch.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)[0]; lt.GetOwnerId() != id || lt.GetOwnerName() != "Liam" || len(lt.GetItems()) != len(pouch.GetItems()) {
			t.Fatalf("loot %+v; want the pouch's items taken by Liam", lt)
		}
		if watcher.Pump(time.Second, nil); len(watcher.Loots) != 0 || len(watcher.Seen) != 0 {
			t.Fatalf("the watcher heard loot %v and saw pouches %v; want nothing of a stranger's", watcher.Loots, watcher.Seen)
		}
		var lootEvents, distinctKeys int
		if err := s.DB.QueryRow(context.Background(), `
			SELECT count(*), count(DISTINCT e.cause_id) FROM item_events e JOIN items i ON i.id = e.item_id
			WHERE i.owner_id = $1 AND e.cause_type = 'drop'`, id).Scan(&lootEvents, &distinctKeys); err != nil {
			t.Fatal(err)
		}
		if lootEvents != len(pouch.GetItems()) || lootEvents != distinctKeys {
			t.Fatalf("%d loot events under %d keys; want one per item taken, %d", lootEvents, distinctKeys, len(pouch.GetItems()))
		}
		for _, it := range pouch.GetItems() {
			if rows, _, _ := s.ItemTotals(t, id, it.GetTemplate()); rows != 1 {
				t.Fatalf("%s: %d rows, want the one stack", it.GetTemplate(), rows)
			}
		}
		want := s.InventoryTotals(t, id)
		_, w := s.Welcome(t, "liam", id)
		held := map[string]int{}
		for _, it := range w.Inventory {
			held[it.Template] += int(it.GetQuantity())
		}
		if !maps.Equal(held, want) {
			t.Fatalf("welcome inventory holds %v, the database %v", held, want)
		}
		s.CheckInvariants(t)
	})

	t.Run("D5/a-grant-retried-under-its-key-gives-one-item-whatever-the-batch", func(t *testing.T) {
		t.Parallel()
		// The world's exact item call, made again with the item's key regrouped beside a new one, as a retry after a lost reply would.
		s := newStack(t)
		_, id := s.CreateCharacter(t, "noah", "Noah", "warrior")
		actor := s.Actor(t, id)
		drop := func(key uuid.UUID) item.AddRequest {
			return item.AddRequest{Key: key.String(), TemplateID: "slime_gel", Quantity: 2, Cause: item.Cause{Type: item.CauseDrop, ID: key}}
		}
		key, other := uuidv7.New(), uuidv7.New()
		first, err := s.Items.AddMany(context.Background(), actor, []item.AddRequest{drop(key)})
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.Items.AddMany(context.Background(), actor, []item.AddRequest{drop(other), drop(key)})
		if err != nil {
			t.Fatal(err)
		}
		if o := first.Outcomes[0]; o.Err != nil || o.Replayed {
			t.Fatalf("the first try: %+v", o)
		}
		replay := again.Outcomes[1]
		if fresh := again.Outcomes[0]; fresh.Err != nil || fresh.Replayed || replay.Err != nil || !replay.Replayed ||
			replay.Result.Revision != first.Revision || replay.Result.Changed[0].Item.ID != first.Changed[0].Item.ID {
			t.Fatalf("the retry answered %+v; want the new item granted and the old one its stored reply", again.Outcomes)
		}
		if rows, quantity, events := s.ItemTotals(t, id, "slime_gel"); rows != 1 || quantity != 4 || events != 2 {
			t.Fatalf("%d rows holding %d with %d events; want one stack of the two, one event each", rows, quantity, events)
		}
		s.CheckInvariants(t)
	})

	t.Run("E3/a-party-takes-its-pouches-in-turn-and-a-member-who-died-gets-none", func(t *testing.T) {
		t.Parallel()
		// The slimes hit for 1 and no one regains HP: Ravi stays one hit from death, and Pia outlasts the slimes that come after her.
		gentle := []e2etest.DataEdit{
			{File: "mobs.yaml", Old: "    hp: 1\n    attack: 6\n", New: "    hp: 1\n    attack: 1\n"},
			{File: "stats.yaml", Old: "vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0.05}", New: "vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0}"},
		}
		s := newStackOver(t, e2etest.GameData(t, slices.Concat(e2etest.QuickSlimes(), []e2etest.DataEdit{slimesDrop(oneGel), everyKill}, gentle)...))
		var ls []*e2etest.Looter
		for _, n := range []struct {
			user, name, class string
			at                gamedata.Point
			hp                int
		}{{"pia", "Pia", "bowman", e2etest.SlimeField, 134}, {"ravi", "Ravi", "warrior", e2etest.OutOfSight, 1}} { // Ravi waits where no mob sees him
			token, id := s.CreateCharacter(t, n.user, n.name, n.class)
			s.Place(t, id, "forest", n.at, n.hp, 0)
			p, snap := s.Join(t, token, id)
			ls = append(ls, e2etest.NewLooter(p, snap))
		}
		pia, ravi := ls[0], ls[1]
		if r := pia.Command(t, "inv", &clientv1.PartyInvite{CharacterId: ravi.ID}); !r.GetOk() {
			t.Fatalf("invite: %+v", r)
		}
		inv := e2etest.Take(t, ravi, "PartyInvited", &ravi.Invites, 5*time.Second, nil)
		if r := ravi.Command(t, "acc", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
			t.Fatalf("accept: %+v", r)
		}
		pia.PartyState(t, 2)
		ravi.PartyState(t, 2) // the world's copy of the party follows the social module's stream
		time.Sleep(500 * time.Millisecond)

		// Pia kills; each kill's pouch goes in turn, from the first member in join order: Pia, Ravi, Pia... Each sees only their own.
		if !pia.HuntPouches(30*time.Second, 2) {
			t.Fatalf("%d pouches of Pia's in 30 s of hunting, want 2", len(pia.Seen))
		}
		ravi.Pump(time.Second, nil)
		owner := map[uint32]string{}
		for _, l := range ls {
			for id := range l.Seen {
				owner[id] = l.ID
			}
		}
		ids := slices.Sorted(maps.Keys(owner))
		for k, id := range ids {
			if want := []string{pia.ID, ravi.ID}[k%2]; owner[id] != want {
				t.Fatalf("pouch %d of %v is %s's, want %s's: the turn", id, ids, owner[id], want)
			}
		}
		// Pia takes her first; both members hear it, naming her.
		first := pia.Seen[ids[0]]
		if r := pia.GoTake(t, first); !r.GetOk() || slices.ContainsFunc(e2etest.Codes(r), func(c int32) bool { return c != 0 }) {
			t.Fatalf("Pia's take answered %+v", r)
		}
		heard := func() bool {
			for _, l := range ls {
				if got := l.LootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN); len(got) != 1 || got[0].GetOwnerId() != pia.ID {
					return false
				}
			}
			return true
		}
		for deadline := time.Now().Add(5 * time.Second); !heard(); ravi.Pump(10*time.Millisecond, nil) {
			if time.Now().After(deadline) {
				t.Fatalf("loot %v and %v; want both members to hear Pia took her pouch", pia.Loots, ravi.Loots)
			}
			pia.Pump(40*time.Millisecond, nil)
		}
		if _, gels, _ := s.ItemTotals(t, pia.ID, "slime_gel"); gels != 1 {
			t.Fatalf("Pia holds %d gels, want the one she took", gels)
		}

		// Ravi walks into the slimes, which kill him; dead or in town, he gets no turn: Pia's next pouches follow one another.
		dead := func(m wsconn.Message) bool {
			d, ok := m.Data.(*clientv1.Died)
			return ok && d.GetCharacterId() == ravi.ID
		}
		for deadline := time.Now().Add(30 * time.Second); !ravi.Pump(50*time.Millisecond, dead); pia.Pump(10*time.Millisecond, nil) {
			if time.Now().After(deadline) {
				t.Fatalf("Ravi never died among the slimes (at %.2f, %.2f, %.2f, %d HP)", ravi.X, ravi.Y, ravi.Z, ravi.HP)
			}
			if err := ravi.Chase(); err != nil {
				t.Fatal(err)
			}
		}
		before := len(pia.Seen)
		if !pia.HuntPouches(30*time.Second, before+2) {
			t.Fatalf("no new pouches in 30 s of hunting after Ravi died")
		}
		after := slices.Sorted(maps.Keys(pia.Seen))[before:]
		if after[len(after)-1]-after[0] != uint32(len(after)-1) {
			t.Fatalf("Pia's new pouches %v leave gaps: one went to Ravi, dead or gone", after)
		}
		s.CheckInvariants(t)
	})

	t.Run("D1/a-full-tab-leaves-the-item-in-its-pouch-until-there-is-room-or-its-life-ends", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), slimesDrop(oneCap), e2etest.LootRules(1, "15s"))...))
		token, id := s.CreateCharacter(t, "sol", "Sol", "bowman")
		s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
		for range 24 {
			s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		l, _ := s.Hello(t, token, id)
		first, ok := l.Hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap fell in 30 s of hunting")
		}
		if r := l.GoTake(t, first); !r.GetOk() || !slices.Equal(e2etest.Codes(r), []int32{code("EQUIP_TAB_FULL")}) {
			t.Fatalf("taking the cap answered %+v, want it refused for the full Equip tab", r)
		}
		if _, still := l.Pouches[first.GetId()]; !still || l.Inventory != 0 {
			t.Fatalf("the cap left its pouch, or something reached the full Equip tab (%d changes)", l.Inventory)
		}

		// A discard makes room: the cap is taken.
		var held string
		if err := s.DB.QueryRow(context.Background(), `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' LIMIT 1`, id).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if r := l.Command(t, "d-1", &clientv1.InventoryDiscard{IdempotencyKey: e2etest.Key(), ItemId: held, Quantity: 1}); !r.GetOk() {
			t.Fatalf("discard: %+v", r)
		}
		if r := l.Take(t, e2etest.All(first)...); !r.GetOk() || !slices.Equal(e2etest.Codes(r), []int32{0}) {
			t.Fatalf("taking the cap with room answered %+v", r)
		}
		if !l.Until(5*time.Second, func() bool {
			return l.Gone[first.GetId()] && len(l.LootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)) == 1
		}) {
			t.Fatalf("gone %v, loot %v; want the cap taken once there was room", l.Gone, l.Loots)
		}

		// The tab is full again: the next cap stays in its pouch through its 15 s life, then is said left, never taken.
		next, ok := l.Hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no second cap in 30 s of hunting")
		}
		if r := l.GoTake(t, next); !slices.Equal(e2etest.Codes(r), []int32{code("EQUIP_TAB_FULL")}) {
			t.Fatalf("taking the second cap answered %+v, want the full Equip tab", r)
		}
		if !l.Until(30*time.Second, func() bool { // its life, in the server's ticks
			left := l.LootOf(next.GetId(), clientv1.LootKind_LOOT_KIND_LEFT)
			return l.Gone[next.GetId()] && len(left) == 1 && slices.Equal(e2etest.Templates(left[0]), []string{"leather_cap"})
		}) {
			t.Fatalf("the second cap's pouch never went with the cap said left (loot %v)", l.Loots)
		}
		var caps int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' AND location = 'inventory'`, id).Scan(&caps); err != nil {
			t.Fatal(err)
		}
		if caps != 24 {
			t.Fatalf("%d caps held, want the 24: one discarded, one taken", caps)
		}
		s.CheckInvariants(t)
	})

	t.Run("D1/a-pouch-stays-on-its-map-while-its-owner-is-away", func(t *testing.T) {
		t.Parallel()
		byThePortal := e2etest.DataEdit{File: "maps/forest.yaml", Old: "{name: slimes, at: [20, 12], radius: 6,", New: "{name: slimes, at: [10, 21], radius: 2,"} // its slimes see a hunter by the portal and come to them
		s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), slimesDrop(oneGel), everyKill, byThePortal)...))
		token, id := s.CreateCharacter(t, "uma", "Uma", "bowman")
		s.Place(t, id, "forest", gamedata.Point{X: 6, Z: 21}, 134, 0) // a second's walk from the forest's way back to the town
		p, snap := s.Join(t, token, id)
		l := e2etest.NewLooter(p, snap)
		gel, ok := l.Hunt(30*time.Second, "slime_gel")
		if !ok {
			t.Fatal("no gel in 30 s of hunting")
		}
		if town := l.Hop(t, e2etest.ForestFromTown, "town", e2etest.TownPortal); len(town.GetPouches()) != 0 {
			t.Fatalf("the town's Snapshot holds pouches %v", town.GetPouches())
		}
		back := l.Hop(t, e2etest.TownPortal, "forest", e2etest.ForestFromTown)
		if !slices.ContainsFunc(back.GetPouches(), func(p *clientv1.PouchView) bool { return p.GetId() == gel.GetId() }) {
			t.Fatalf("back in the forest to pouches %v; want the gel's still there", back.GetPouches())
		}
		if r := l.GoTake(t, gel); !r.GetOk() || !slices.Equal(e2etest.Codes(r), []int32{0}) {
			t.Fatalf("taking the gel answered %+v", r)
		}
		got := l.LootOf(gel.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)
		if !l.Until(5*time.Second, func() bool { got = l.LootOf(gel.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN); return len(got) == 1 }) || got[0].GetMapId() != "forest" || got[0].GetOwnerId() != id {
			t.Fatalf("loot %v; want the forest's gel taken by Uma", l.Loots)
		}
		if _, quantity, _ := s.ItemTotals(t, id, "slime_gel"); quantity < 1 {
			t.Fatal("the gel is not in the inventory")
		}
		s.CheckInvariants(t)
	})

	t.Run("D2/move-and-discard-over-the-websocket", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		_, id := s.CreateCharacter(t, "olive", "Olive", "warrior")
		p, welcome := s.Welcome(t, "olive", id)
		var potions *clientv1.ItemView
		for _, it := range welcome.Inventory {
			if it.Template == "red_potion" {
				potions = it
			}
		}
		if potions.GetItemId() == "" || potions.GetQuantity() != 5 || potions.GetTab() != "use" || potions.GetSlot() != 0 {
			t.Fatalf("starter potions %+v in %+v", potions, welcome.Inventory)
		}
		if welcome.InventoryRevision != 1 {
			t.Fatalf("welcome at revision %d, want 1: the starter kit is the inventory's first change", welcome.InventoryRevision)
		}

		keys := map[string]string{} // each command's key, as the client makes them: k- and a UUID
		for _, name := range []string{"move-1", "move-3", "discard-1", "discard-2"} {
			keys[name] = "k-" + uuidv7.NewString()
		}
		p.Send(wsconn.Message{ReqID: "m-1", Data: &clientv1.InventoryMove{IdempotencyKey: keys["move-1"], ItemId: potions.GetItemId(), Tab: "use", Slot: 3}})
		r := p.Expect("Response")
		if r.ReqID != "m-1" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("move response %+v", r)
		}
		ic := p.Expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
		if len(ic.Slots) != 2 || ic.Slots[0].Slot != 0 || ic.Slots[0].Item != nil || ic.Slots[1].Slot != 3 || ic.Slots[1].GetItem().GetQuantity() != 5 || ic.Revision != 2 {
			t.Fatalf("after move %+v, want revision 2", ic)
		}
		p.Send(wsconn.Message{ReqID: "m-2", Data: &clientv1.InventoryMove{IdempotencyKey: keys["move-1"], ItemId: potions.GetItemId(), Tab: "use", Slot: 3}})
		if r := p.Expect("Response"); r.ReqID != "m-2" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("replayed move %+v", r)
		}
		if ic := p.Expect("InventoryChanged").Data.(*clientv1.InventoryChanged); ic.Revision != 2 {
			t.Fatalf("replayed move at revision %d, want the 2 it took", ic.Revision)
		}
		p.Send(wsconn.Message{ReqID: "m-3", Data: &clientv1.InventoryMove{IdempotencyKey: keys["move-3"], ItemId: potions.GetItemId(), Tab: "etc", Slot: 1}})
		if r := p.Expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != code("WRONG_TAB") {
			t.Fatalf("cross-tab move %+v", r)
		}
		p.Send(wsconn.Message{ReqID: "d-1", Data: &clientv1.InventoryDiscard{IdempotencyKey: keys["discard-1"], ItemId: potions.GetItemId(), Quantity: 2}})
		if r := p.Expect("Response").Data.(*clientv1.Response); !r.GetOk() {
			t.Fatalf("discard %+v", r)
		}
		ic = p.Expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].Slot != 3 || ic.Slots[0].Item == nil || ic.Slots[0].GetItem().GetQuantity() != 3 || ic.Revision != 3 {
			t.Fatalf("after discard %+v, want revision 3", ic)
		}
		p.Send(wsconn.Message{ReqID: "d-2", Data: &clientv1.InventoryDiscard{IdempotencyKey: keys["discard-2"], ItemId: potions.GetItemId(), Quantity: 9}})
		if r := p.Expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != code("BAD_QUANTITY") {
			t.Fatalf("over-discard %+v", r)
		}
		if rows, quantity, _ := s.ItemTotals(t, id, "red_potion"); rows != 1 || quantity != 3 {
			t.Fatalf("%d rows holding %d potions", rows, quantity)
		}
		s.CheckInvariants(t)
	})
}

// A kill -9 while takes and swaps are in flight loses and duplicates no item; the players reconnect to the restarted game and are welcomed with what the database holds.
func TestCrashDuringTakes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds and runs the game as its own process")
	}
	s, game := processStack(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), everyKill)...))

	names := [][2]string{{"pat", "Pat"}, {"quinn", "Quinn"}, {"rae", "Rae"}}
	var hunters []*e2etest.Looter
	for k, n := range names {
		token, id := s.CreateCharacter(t, n[0], n[1], "warrior")
		s.Place(t, id, "forest", gamedata.Point{X: e2etest.SlimeField.X, Z: e2etest.SlimeField.Z + 1.5*float64(k-1)}, 144, 0)
		hunters = append(hunters, e2etest.NewLooter(s.Join(t, token, id)))
	}
	var wg sync.WaitGroup
	took, swapped := make([]int, len(hunters)), make([]int, len(hunters))
	for k, h := range hunters {
		swap := s.Swapper(t, h.ID)
		wg.Go(func() { took[k] = h.Farm(10 * time.Second) })
		wg.Go(func() { swapped[k] = swap(6 * time.Second) }) // on for a second past the kill
	}
	// Hard-kill the game while takes and swaps are in flight; the swaps go on through the item module beside it.
	time.Sleep(5 * time.Second)
	game.kill()
	wg.Wait()
	t.Logf("inventory changes before the kill: %v; swaps: %v", took, swapped)
	s.CheckInvariants(t)

	game.start()
	s.CheckInvariants(t)
	for k, n := range names {
		_, w := s.Welcome(t, n[0], hunters[k].ID)
		held := map[string]int{}
		for _, it := range w.Inventory {
			held[it.GetTemplate()] += int(it.GetQuantity())
		}
		if want := s.InventoryTotals(t, hunters[k].ID); !maps.Equal(held, want) {
			t.Fatalf("%s is welcomed with %v, the database holds %v", n[1], held, want)
		}
	}
	if out := game.out.String(); strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("the game logged errors:\n%s", out)
	}
}
