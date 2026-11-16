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

// lootTimes sets loot.yaml's three timings.
func lootTimes(collectDelay, dropLife, roomCheck string) e2etest.DataEdit {
	return e2etest.DataEdit{File: "loot.yaml", New: "collect_delay: " + collectDelay + "\ndrop_life: " + dropLife + "\nroom_check_every: " + roomCheck + "\n"}
}

const (
	oneGel = "      - {item: slime_gel, chance: 1, min: 1, max: 1}\n"
	oneCap = "      - {item: leather_cap, chance: 1, min: 1, max: 1}\n"
)

func TestLootAndKeep(t *testing.T) {
	t.Parallel()
	t.Run("D1-D2/a-kill-drops-an-item-everyone-sees-and-it-flies-to-its-killer-as-one-item", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, e2etest.QuickSlimes()...))
		token, id := s.CreateCharacter(t, "liam", "Liam", "bowman")
		s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
		p, snap := s.Join(t, token, id)
		killer := e2etest.NewLooter(p, snap)
		token2, id2 := s.CreateCharacter(t, "mia", "Mia", "bowman")
		s.Place(t, id2, "forest", gamedata.Point{X: e2etest.SlimeField.X - 0.625, Z: e2etest.SlimeField.Z}, 134, 0)
		wp, wsnap := s.Join(t, token2, id2)
		watcher := e2etest.NewLooter(wp, wsnap)
		killer.Pump(300*time.Millisecond, nil)

		drop, ok := killer.Hunt(30*time.Second, "")
		if !ok {
			t.Fatal("no drop in 30 s of hunting")
		}
		// The killer stands still: a kill is within the bow's 13.125 m and a slime's 0.5 m of it, and its drop within 0.75 m of that, on the ground.
		data, err := load.Dir(s.Data)
		if err != nil {
			t.Fatal(err)
		}
		forest, _ := data.Map("forest")
		if drop.Y != forest.Geometry.Terrain.Height(drop.X, drop.Z) || math.Hypot(drop.X-killer.X, drop.Z-killer.Z) > 13.125+0.5+0.75 || drop.Quantity < 1 || (drop.Template != "slime_gel" && drop.Template != "red_potion") {
			t.Fatalf("drop %+v, the killer at (%v, %v)", drop, killer.X, killer.Z)
		}
		if !watcher.Pump(3*time.Second, func(wsconn.Message) bool { _, seen := watcher.Drops[drop.GetId()]; return seen }) {
			t.Fatalf("the watcher never saw drop %d", drop.GetId())
		}

		// No key: the drop flies to its killer within a few seconds, one item per drop.
		if !killer.Until(10*time.Second, func() bool { return killer.Inventory > 0 }) {
			t.Fatalf("no InventoryChanged within 10 s of the kill (failures %+v)", killer.Failures)
		}
		if !killer.Settle(10 * time.Second) { // the kill's other drops, if any, and a queued shot's
			t.Fatalf("Liam's drops still down after 10 s: %v", killer.Drops)
		}
		if len(killer.Failures) != 0 {
			t.Fatalf("failures %+v", killer.Failures)
		}
		if !killer.Collected[drop.GetId()] || !slices.ContainsFunc(killer.Loots, func(l *clientv1.Loot) bool {
			return l.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && l.GetDropId() == drop.GetId() && l.GetOwnerId() == id && l.GetOwnerName() == "Liam"
		}) {
			t.Fatalf("collected %v, loot %v; want drop %d collected for Liam", killer.Collected, killer.Loots, drop.GetId())
		}
		if drop.GetOwnerId() != id {
			t.Fatalf("the drop shows owner %q, want its killer", drop.GetOwnerId())
		}
		if !watcher.Until(3*time.Second, func() bool { return watcher.Collected[drop.GetId()] }) || len(watcher.Loots) != 0 {
			t.Fatalf("the watcher saw collected %v and loot %v; want the drop collected, and no loot of a stranger's", watcher.Collected, watcher.Loots)
		}
		var lootEvents, distinctDrops int
		if err := s.DB.QueryRow(context.Background(), `
			SELECT count(*), count(DISTINCT e.cause_id) FROM item_events e JOIN items i ON i.id = e.item_id
			WHERE i.owner_id = $1 AND e.cause_type = 'drop'`, id).Scan(&lootEvents, &distinctDrops); err != nil {
			t.Fatal(err)
		}
		if lootEvents == 0 || lootEvents != distinctDrops {
			t.Fatalf("%d loot events under %d drop IDs; want one per drop", lootEvents, distinctDrops)
		}
		if rows, _, _ := s.ItemTotals(t, id, drop.Template); rows != 1 {
			t.Fatalf("%s: %d rows, want the one stack", drop.Template, rows)
		}
		wantQty := s.InventoryTotals(t, id)[drop.Template]
		_, w := s.Welcome(t, "liam", id)
		held := 0
		for _, it := range w.Inventory {
			if it.Template == drop.Template {
				held += int(it.GetQuantity())
			}
		}
		if held != wantQty {
			t.Fatalf("welcome inventory holds %d %s, want %d: %+v", held, drop.Template, wantQty, w.Inventory)
		}
		s.CheckInvariants(t)
	})

	t.Run("D5/a-grant-retried-under-its-key-gives-one-item-whatever-the-batch", func(t *testing.T) {
		t.Parallel()
		// The world's exact item call, made again with the drop's key regrouped beside a new drop, as a retry after a lost reply would.
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
			t.Fatalf("the retry answered %+v; want the new drop granted and the old one its stored reply", again.Outcomes)
		}
		if rows, quantity, events := s.ItemTotals(t, id, "slime_gel"); rows != 1 || quantity != 4 || events != 2 {
			t.Fatalf("%d rows holding %d with %d events; want one stack of the two drops, one event each", rows, quantity, events)
		}
		s.CheckInvariants(t)
	})

	t.Run("E3/a-party-takes-its-drops-in-turn-and-a-member-who-died-gets-none", func(t *testing.T) {
		t.Parallel()
		// The slimes hit for 1 and no one regains HP: Ravi stays one hit from death, and Pia outlasts the slimes that come after her.
		gentle := []e2etest.DataEdit{
			{File: "mobs.yaml", Old: "    hp: 1\n    attack: 6\n", New: "    hp: 1\n    attack: 1\n"},
			{File: "stats.yaml", Old: "vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0.05}", New: "vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0}"},
		}
		s := newStackOver(t, e2etest.GameData(t, slices.Concat(e2etest.QuickSlimes(), []e2etest.DataEdit{slimesDrop(oneGel)}, gentle)...))
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

		// Pia kills; her drops go in turn, from the first member in join order: Pia, Ravi, Pia...
		if !pia.HuntDrops(30*time.Second, 2) {
			t.Fatalf("%d drops in 30 s of hunting, want 2", len(pia.Seen))
		}
		// Each member on the map hears each drop collected, a second after its kill.
		heard := func() bool {
			for id := range pia.Seen {
				for _, l := range ls {
					if len(l.LootOf(id, clientv1.LootKind_LOOT_KIND_COLLECTED)) == 0 {
						return false
					}
				}
			}
			return true
		}
		for deadline := time.Now().Add(5 * time.Second); !heard(); ravi.Pump(10*time.Millisecond, nil) {
			if time.Now().After(deadline) {
				t.Fatalf("loot %v and %v; want each drop heard collected by both members", pia.Loots, ravi.Loots)
			}
			pia.Pump(40*time.Millisecond, nil)
		}
		ids := slices.Sorted(maps.Keys(pia.Seen))
		owned := map[string]int{}
		// heardOnce checks each member heard each of ids collected once, for its owner in turn.
		heardOnce := func() {
			t.Helper()
			for k, id := range ids {
				want := []string{pia.ID, ravi.ID}[k%2]
				for _, l := range ls {
					if got := l.LootOf(id, clientv1.LootKind_LOOT_KIND_COLLECTED); len(got) != 1 || got[0].GetOwnerId() != want {
						t.Fatalf("%s heard %v of drop %d, want it collected for %s", l.ID, got, id, want)
					}
				}
			}
		}
		for k, id := range ids {
			want := []string{pia.ID, ravi.ID}[k%2]
			if got := pia.Seen[id].GetOwnerId(); got != want {
				t.Fatalf("drop %d of %v is %s's, want %s's: the turn", id, ids, got, want)
			}
			owned[want]++
		}
		heardOnce()
		for _, l := range ls {
			if _, quantity, _ := s.ItemTotals(t, l.ID, "slime_gel"); quantity != owned[l.ID] {
				t.Fatalf("%s holds %d gel, want the %d drops that were theirs", l.ID, quantity, owned[l.ID])
			}
		}

		// Ravi walks into the slimes, which kill him; dead or in town, he gets no turn.
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
		if !pia.HuntDrops(30*time.Second, before+2) {
			t.Fatalf("no new drops in 30 s of hunting after Ravi died")
		}
		for _, id := range slices.Sorted(maps.Keys(pia.Seen))[before:] {
			if got := pia.Seen[id].GetOwnerId(); got != pia.ID {
				t.Fatalf("drop %d went to %s with Ravi dead or gone", id, got)
			}
		}
		heardOnce() // a second Loot for an early drop would have come by now
		s.CheckInvariants(t)
	})

	t.Run("D1/a-full-inventory-keeps-a-drop-waiting-until-there-is-room-or-its-life-ends", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), slimesDrop(oneCap), lootTimes("1s", "6s", "1s"))...))
		token, id := s.CreateCharacter(t, "sol", "Sol", "bowman")
		s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
		for range 24 {
			s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		l, _ := s.Hello(t, token, id)
		first, ok := l.Hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap dropped in 30 s of hunting")
		}
		if !l.Until(5*time.Second, func() bool { return len(l.LootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_WAITING)) == 1 }) {
			t.Fatalf("loot %v; want the cap waiting for room", l.Loots)
		}
		if _, still := l.Drops[first.GetId()]; !still || l.Inventory != 0 {
			t.Fatalf("the cap left the ground, or something reached the full Equip tab (%d changes)", l.Inventory)
		}

		// A discard makes room: the waiting cap arrives within a room check.
		var held string
		if err := s.DB.QueryRow(context.Background(), `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' LIMIT 1`, id).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if r := l.Command(t, "d-1", &clientv1.InventoryDiscard{IdempotencyKey: e2etest.Key(), ItemId: held, Quantity: 1}); !r.GetOk() {
			t.Fatalf("discard: %+v", r)
		}
		// A tick's frame goes before its owner's events: wait for the Loot, which follows collected_drops.
		if !l.Until(10*time.Second, func() bool { // a 1 s room check, in the server's ticks
			return l.Collected[first.GetId()] && len(l.LootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)) == 1
		}) {
			t.Fatalf("collected %v, loot %v; want the cap collected once there was room", l.Collected, l.Loots)
		}

		// The tab is full again: the next cap waits out its life and is lost, never collected.
		next, ok := l.Hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no second cap in 30 s of hunting")
		}
		if !l.Until(30*time.Second, func() bool { // its 6 s life, in the server's ticks
			return l.Removed[next.GetId()] && len(l.LootOf(next.GetId(), clientv1.LootKind_LOOT_KIND_LOST)) == 1
		}) {
			t.Fatalf("the second cap never left the ground as lost (loot %v)", l.Loots)
		}
		if l.Collected[next.GetId()] {
			t.Fatalf("loot %v; want the second cap lost, and not collected", l.Loots)
		}
		var caps int
		if err := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' AND location = 'inventory'`, id).Scan(&caps); err != nil {
			t.Fatal(err)
		}
		if caps != 24 {
			t.Fatalf("%d caps held, want the 24: one discarded, one collected", caps)
		}
		s.CheckInvariants(t)
	})

	t.Run("D1/an-owner-who-takes-a-portal-before-collection-still-gets-the-drop", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), slimesDrop(oneGel), lootTimes("4s", "60s", "2s"))...))
		token, id := s.CreateCharacter(t, "uma", "Uma", "bowman")
		s.Place(t, id, "forest", gamedata.Point{X: 6, Z: 21}, 134, 0) // a second's walk from the forest's way back to the town
		p, snap := s.Join(t, token, id)
		l := e2etest.NewLooter(p, snap)
		gel, ok := l.Hunt(30*time.Second, "slime_gel")
		if !ok {
			t.Fatal("no gel in 30 s of hunting")
		}
		l.Hop(t, e2etest.ForestFromTown, "town", e2etest.TownPortal) // within the 4 s before the gel flies
		if !l.Until(8*time.Second, func() bool {
			return l.Inventory > 0 && len(l.LootOf(gel.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)) == 1 // the Loot follows its InventoryChanged
		}) {
			t.Fatalf("no InventoryChanged and Loot in the town (loot %v)", l.Loots)
		}
		got := l.LootOf(gel.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)
		if len(got) != 1 || got[0].GetMapId() != "forest" || got[0].GetOwnerId() != id {
			t.Fatalf("loot %v; want the forest's gel collected for Uma", l.Loots)
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

// A kill -9 while grants and swaps are in flight loses and duplicates no item; the players reconnect to the restarted game and are welcomed with what the database holds.
func TestCrashDuringPickups(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds and runs the game as its own process")
	}
	s, game := processStack(t, e2etest.GameData(t, e2etest.QuickSlimes()...))

	names := [][2]string{{"pat", "Pat"}, {"quinn", "Quinn"}, {"rae", "Rae"}}
	var hunters []*e2etest.Looter
	for k, n := range names {
		token, id := s.CreateCharacter(t, n[0], n[1], "warrior")
		s.Place(t, id, "forest", gamedata.Point{X: e2etest.SlimeField.X, Z: e2etest.SlimeField.Z + 1.5*float64(k-1)}, 144, 0)
		hunters = append(hunters, e2etest.NewLooter(s.Join(t, token, id)))
	}
	var wg sync.WaitGroup
	collected, swapped := make([]int, len(hunters)), make([]int, len(hunters))
	for k, h := range hunters {
		swap := s.Swapper(t, h.ID)
		wg.Go(func() { collected[k] = h.Farm(10 * time.Second) })
		wg.Go(func() { swapped[k] = swap(6 * time.Second) }) // on for a second past the kill
	}
	// Hard-kill the game while grants and swaps are in flight; the swaps go on through the item module beside it.
	time.Sleep(5 * time.Second)
	game.kill()
	wg.Wait()
	t.Logf("inventory changes before the kill: %v; swaps: %v", collected, swapped)
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
