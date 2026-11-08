package e2e_test

import (
	"context"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	clientv1 "finga/api/gen/client/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/protocol"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
	"finga/services/item/itemtest"
)

// looter is a player tracking its position and the drops on the ground from every message.
type looter struct {
	*player
	x, y      float64
	hp        int32
	drops     map[uint32]*clientv1.DropView
	inventory int // InventoryChanged messages seen
	// picked are drops removed in a tick that also brought this looter an InventoryChanged.
	picked      map[uint32]*clientv1.DropView
	removedTick uint64
	removed     []*clientv1.DropView // the drops the Delta of removedTick took away
	failures    []*clientv1.Response
	seq         uint32

	// Recorded for the social rows, consumed by their helpers.
	responses   []reply
	partyStates []*clientv1.PartyState
	invites     []*clientv1.PartyInvited
	chatLines   []*clientv1.ChatMessage
	exp         []*clientv1.ExpGained
}

func newLooter(p *player, snap *clientv1.Snapshot) *looter {
	l := &looter{player: p, drops: map[uint32]*clientv1.DropView{}, picked: map[uint32]*clientv1.DropView{}, seq: 2000}
	for _, d := range snap.Drops {
		l.drops[d.GetId()] = d
	}
	for _, v := range snap.Players {
		if v.GetId() == p.id {
			l.x, l.y, l.hp = v.X, v.Y, v.GetHp()
		}
	}
	return l
}

func (l *looter) observe(m wsconn.Message) {
	switch d := m.Data.(type) {
	case *clientv1.Delta:
		for _, v := range d.Players {
			if v.GetId() == l.id {
				l.x, l.y, l.hp = v.X, v.Y, v.GetHp()
			}
		}
		for _, dv := range d.Drops {
			l.drops[dv.GetId()] = dv
		}
		l.removedTick, l.removed = m.Tick, l.removed[:0]
		for _, id := range d.RemovedDrops {
			if dv, ok := l.drops[id]; ok {
				l.removed = append(l.removed, dv)
			}
			delete(l.drops, id)
		}
	case *clientv1.Snapshot:
		l.drops = map[uint32]*clientv1.DropView{}
		for _, dv := range d.Drops {
			l.drops[dv.GetId()] = dv
		}
		for _, v := range d.Players {
			if v.GetId() == l.id {
				l.x, l.y, l.hp = v.X, v.Y, v.GetHp()
			}
		}
	case *clientv1.InventoryChanged:
		l.inventory++
		if m.Tick == l.removedTick {
			for _, dv := range l.removed {
				l.picked[dv.GetId()] = dv
			}
		}
	case *clientv1.Response:
		if !d.GetOk() {
			l.failures = append(l.failures, d)
		}
		l.responses = append(l.responses, reply{m.ReqID, d})
	case *clientv1.PartyState:
		l.partyStates = append(l.partyStates, d)
	case *clientv1.PartyInvited:
		l.invites = append(l.invites, d)
	case *clientv1.ChatMessage:
		l.chatLines = append(l.chatLines, d)
	case *clientv1.ExpGained:
		l.exp = append(l.exp, d)
	}
}

type reply struct {
	req string
	r   *clientv1.Response
}

// until pumps in short slices, for up to d, until pick finds (and consumes) what it wants.
func (l *looter) until(d time.Duration, pick func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if pick() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		l.pump(50*time.Millisecond, nil)
	}
}

func (l *looter) pump(d time.Duration, pred func(wsconn.Message) bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		m, ok := l.read(time.Until(deadline))
		if !ok {
			return false
		}
		l.observe(m)
		if pred != nil && pred(m) {
			return true
		}
	}
	return false
}

// trySend is send without failing the test, for goroutines.
func (l *looter) trySend(m wsconn.Message) error {
	b, err := l.codec.Encode(m)
	if err != nil {
		return err
	}
	return l.conn.Write(context.Background(), websocket.MessageBinary, b)
}

func (l *looter) input(in *clientv1.Input) error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: in})
}

func (l *looter) attack() error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: &clientv1.Attack{}})
}

func (l *looter) pickUp() error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: &clientv1.PickUp{}})
}

// nearestDrop is the closest drop of the template ("" for any).
func (l *looter) nearestDrop(template string) (*clientv1.DropView, bool) {
	var best *clientv1.DropView
	bestDist := math.Inf(1)
	for _, d := range l.drops {
		if template != "" && d.Template != template {
			continue
		}
		if dist := math.Abs(d.X - l.x); dist < bestDist {
			best, bestDist = d, dist
		}
	}
	return best, bestDist < math.Inf(1)
}

// hunt attacks every 850 ms until a drop of the template appears.
func (l *looter) hunt(d time.Duration, template string) (*clientv1.DropView, bool) {
	deadline := time.Now().Add(d)
	next := time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(next) {
			if err := l.attack(); err != nil {
				return nil, false
			}
			next = time.Now().Add(850 * time.Millisecond)
		}
		l.pump(100*time.Millisecond, nil)
		if drop, ok := l.nearestDrop(template); ok {
			return drop, true
		}
	}
	return nil, false
}

// walkTo walks to within 12 px of x and stops there, re-aiming after knockback and jumping when stuck.
func (l *looter) walkTo(x float64, d time.Duration) bool {
	deadline := time.Now().Add(d)
	lastX, stuck := l.x, 0
	for time.Now().Before(deadline) {
		dx := x - l.x
		if math.Abs(dx) < 12 {
			if err := l.input(&clientv1.Input{}); err != nil {
				return false
			}
			l.pump(100*time.Millisecond, nil)
			if math.Abs(x-l.x) < 24 {
				return true
			}
			continue // knocked back while stopping
		}
		if math.Abs(l.x-lastX) < 1 {
			stuck++
		} else {
			stuck = 0
		}
		lastX = l.x
		if err := l.input(&clientv1.Input{Right: dx > 0, Left: dx < 0, Jump: stuck >= 3}); err != nil {
			return false
		}
		l.pump(50*time.Millisecond, nil)
	}
	_ = l.input(&clientv1.Input{})
	return false
}

func (l *looter) loot(d time.Duration) int {
	deadline := time.Now().Add(d)
	nextAttack := time.Now()
	for time.Now().Before(deadline) {
		if drop, ok := l.nearestDrop(""); ok {
			if math.Abs(drop.X-l.x) >= 12 {
				if l.input(&clientv1.Input{Right: drop.X > l.x, Left: drop.X < l.x}) != nil {
					return l.inventory
				}
			} else {
				_ = l.input(&clientv1.Input{})
				if l.pickUp() != nil {
					return l.inventory
				}
			}
		} else if !time.Now().Before(nextAttack) {
			if l.attack() != nil {
				return l.inventory
			}
			nextAttack = time.Now().Add(850 * time.Millisecond)
		}
		if !l.pump(50*time.Millisecond, nil) && l.closed(0) {
			return l.inventory
		}
	}
	_ = l.input(&clientv1.Input{})
	return l.inventory
}

// welcome says Hello with a fresh login, replacing the old session, and returns the Welcome.
func (s *stack) welcome(t *testing.T, username, id string) (*player, *clientv1.Welcome) {
	t.Helper()
	token := s.call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).body["token"].(string)
	p := s.connect(t, id)
	p.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: id}})
	w := p.expect("Welcome").Data.(*clientv1.Welcome)
	p.token = w.Token
	p.expect("Snapshot")
	return p, w
}

func (s *stack) itemTotals(t *testing.T, owner, template string) (rows int, quantity int, events int) {
	t.Helper()
	err := s.db.QueryRow(context.Background(), `
		SELECT count(*), coalesce(sum(quantity), 0),
		       (SELECT count(*) FROM item_events e JOIN items i ON i.id = e.item_id WHERE i.owner_id = $1 AND i.template_id = $2)
		FROM items WHERE owner_id = $1 AND template_id = $2 AND location = 'inventory'`, owner, template).Scan(&rows, &quantity, &events)
	if err != nil {
		t.Fatal(err)
	}
	return rows, quantity, events
}

func (s *stack) inventoryTotals(t *testing.T, owner string) map[string]int {
	t.Helper()
	rows, err := s.db.Query(context.Background(), `SELECT template_id, sum(quantity) FROM items WHERE owner_id = $1 AND location = 'inventory' GROUP BY template_id`, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var template string
		var quantity int
		if err := rows.Scan(&template, &quantity); err != nil {
			t.Fatal(err)
		}
		out[template] = quantity
	}
	return out
}

func (s *stack) checkInvariants(t *testing.T) {
	t.Helper()
	data, err := gamedata.Load("../data")
	if err != nil {
		t.Fatal(err)
	}
	mismatches, err := itemtest.CheckInvariants(context.Background(), s.db, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("invariant mismatches: %v", mismatches)
	}
}

// starterQuantity is what a new warrior already holds of a template.
func starterQuantity(template string) int {
	if template == "red_potion" {
		return 5
	}
	return 0
}

func TestLootAndKeep(t *testing.T) {
	t.Run("D1-D2/a-kill-drops-an-item-everyone-sees-and-a-double-pickup-gives-exactly-one", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "liam", "Liam", "warrior")
		s.place(t, id, "forest", 380, 608, 204, 0)
		p, snap := s.join(t, token, id)
		killer := newLooter(p, snap)
		token2, id2 := s.createCharacter(t, "mia", "Mia", "bowman")
		s.place(t, id2, "forest", 360, 608, 105, 0)
		wp, wsnap := s.join(t, token2, id2)
		watcher := newLooter(wp, wsnap)
		killer.pump(300*time.Millisecond, nil)
		killer.face(true)

		drop, ok := killer.hunt(90*time.Second, "")
		if !ok {
			t.Fatal("no drop in 90 s of hunting")
		}
		if drop.Y != 608 || drop.X < 150 || drop.X > 850 || drop.Quantity < 1 || (drop.Template != "slime_gel" && drop.Template != "red_potion") {
			t.Fatalf("drop %+v", drop)
		}
		if !watcher.pump(3*time.Second, func(wsconn.Message) bool { _, seen := watcher.drops[drop.GetId()]; return seen }) {
			t.Fatalf("the watcher never saw drop %d", drop.GetId())
		}

		// Two PickUps in one tick: the second finds it reserved or takes another drop; resent if a slime knocks us off.
		got := false
		for attempt := 0; attempt < 20 && !got; attempt++ {
			if !killer.walkTo(drop.X, 15*time.Second) {
				t.Fatalf("could not reach the drop at %.0f (at %.0f)", drop.X, killer.x)
			}
			if err := killer.pickUp(); err != nil {
				t.Fatal(err)
			}
			if err := killer.pickUp(); err != nil {
				t.Fatal(err)
			}
			got = killer.pump(5*time.Second, func(m wsconn.Message) bool {
				return m.Type == "InventoryChanged" || math.Abs(killer.x-drop.X) > 24 // bounced away
			}) && killer.inventory > 0
		}
		if !got {
			t.Fatalf("no InventoryChanged after PickUp (failures %+v, at %.0f, drop %+v)", killer.failures, killer.x, drop)
		}
		killer.pump(time.Second, nil)
		if _, picked := killer.picked[drop.GetId()]; !picked || len(killer.picked) != killer.inventory || len(killer.failures) != 0 {
			t.Fatalf("picked %v with %d InventoryChanged, failures %+v; want drop %d among them, one InventoryChanged each", killer.picked, killer.inventory, killer.failures, drop.GetId())
		}
		if _, still := killer.drops[drop.GetId()]; still {
			t.Fatal("the drop is still on the ground")
		}
		if !watcher.pump(3*time.Second, func(wsconn.Message) bool { _, seen := watcher.drops[drop.GetId()]; return !seen }) {
			t.Fatal("the watcher still sees the drop")
		}
		// Re-sending a PickUp, as after a lost reply, is a new pickup that takes nothing already taken.
		if err := killer.trySend(wsconn.Message{Seq: killer.seq, Data: &clientv1.PickUp{}}); err != nil {
			t.Fatal(err)
		}
		killer.pump(time.Second, nil)
		if len(killer.picked) != killer.inventory || len(killer.failures) != 0 {
			t.Fatalf("after re-sending PickUp with seq %d: %d InventoryChanged for %d drops picked, failures %+v", killer.seq, killer.inventory, len(killer.picked), killer.failures)
		}
		taken := len(killer.picked)
		// One item event per drop, and the stack holds the starter stack plus the drop.
		var lootEvents, distinctDrops int
		if err := s.db.QueryRow(context.Background(), `
			SELECT count(*), count(DISTINCT e.cause_id) FROM item_events e JOIN items i ON i.id = e.item_id
			WHERE i.owner_id = $1 AND e.cause_type = 'drop'`, id).Scan(&lootEvents, &distinctDrops); err != nil {
			t.Fatal(err)
		}
		if lootEvents != taken || distinctDrops != taken {
			t.Fatalf("%d loot events under %d drop IDs for %d drops taken", lootEvents, distinctDrops, taken)
		}
		wantQty, sameTemplate := starterQuantity(drop.Template), 0
		for _, d := range killer.picked {
			if d.Template == drop.Template {
				wantQty += int(d.GetQuantity())
				sameTemplate++
			}
		}
		rows, quantity, events := s.itemTotals(t, id, drop.Template)
		if rows != 1 || quantity != wantQty {
			t.Fatalf("%s: %d rows holding %d, want 1 holding %d", drop.Template, rows, quantity, wantQty)
		}
		if wantEvents := sameTemplate + min(starterQuantity(drop.Template), 1); events != wantEvents {
			t.Fatalf("%s: %d events, want %d", drop.Template, events, wantEvents)
		}

		_, w := s.welcome(t, "liam", id)
		held := 0
		for _, it := range w.Inventory {
			if it.Template == drop.Template {
				held += int(it.GetQuantity())
			}
		}
		if held != wantQty {
			t.Fatalf("welcome inventory holds %d %s, want %d: %+v", held, drop.Template, wantQty, w.Inventory)
		}
		s.checkInvariants(t)
	})

	t.Run("D5/a-retried-pickup-job-gives-one-item", func(t *testing.T) {
		// The worker's exact item-service call, made twice with the drop's key as a retry would.
		s := newStack(t)
		_, id := s.createCharacter(t, "noah", "Noah", "warrior")
		var account string
		if err := s.db.QueryRow(context.Background(), `SELECT account_id FROM characters WHERE id = $1`, id).Scan(&account); err != nil {
			t.Fatal(err)
		}
		key := uuidv7.NewString()
		in := &itemv1.AddRequest{Actor: &itemv1.Actor{AccountId: account, CharacterId: id}, IdempotencyKey: key, TemplateId: "slime_gel", Quantity: 2,
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: key}}
		first, err := s.items.Add(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		second, err := s.items.Add(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.GetChanged()) != 1 || len(second.GetChanged()) != 1 || first.GetChanged()[0].GetItem().GetId() != second.GetChanged()[0].GetItem().GetId() {
			t.Fatalf("first %+v second %+v", first, second)
		}
		if rows, quantity, events := s.itemTotals(t, id, "slime_gel"); rows != 1 || quantity != 2 || events != 1 {
			t.Fatalf("%d rows holding %d with %d events", rows, quantity, events)
		}
		s.checkInvariants(t)
	})

	t.Run("D2/move-and-discard-over-the-websocket", func(t *testing.T) {
		s := newStack(t)
		_, id := s.createCharacter(t, "olive", "Olive", "warrior")
		p, welcome := s.welcome(t, "olive", id)
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
		p.send(wsconn.Message{ReqID: "m-1", Data: &clientv1.InventoryMove{IdempotencyKey: keys["move-1"], ItemId: potions.GetItemId(), Tab: "use", Slot: 3}})
		r := p.expect("Response")
		if r.ReqID != "m-1" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("move response %+v", r)
		}
		ic := p.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
		if len(ic.Slots) != 2 || ic.Slots[0].Slot != 0 || ic.Slots[0].Item != nil || ic.Slots[1].Slot != 3 || ic.Slots[1].GetItem().GetQuantity() != 5 || ic.Revision != 2 {
			t.Fatalf("after move %+v, want revision 2", ic)
		}
		p.send(wsconn.Message{ReqID: "m-2", Data: &clientv1.InventoryMove{IdempotencyKey: keys["move-1"], ItemId: potions.GetItemId(), Tab: "use", Slot: 3}})
		if r := p.expect("Response"); r.ReqID != "m-2" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("replayed move %+v", r)
		}
		if ic := p.expect("InventoryChanged").Data.(*clientv1.InventoryChanged); ic.Revision != 2 {
			t.Fatalf("replayed move at revision %d, want the 2 it took", ic.Revision)
		}
		p.send(wsconn.Message{ReqID: "m-3", Data: &clientv1.InventoryMove{IdempotencyKey: keys["move-3"], ItemId: potions.GetItemId(), Tab: "etc", Slot: 1}})
		if r := p.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != "wrong_tab" {
			t.Fatalf("cross-tab move %+v", r)
		}
		p.send(wsconn.Message{ReqID: "d-1", Data: &clientv1.InventoryDiscard{IdempotencyKey: keys["discard-1"], ItemId: potions.GetItemId(), Quantity: 2}})
		if r := p.expect("Response").Data.(*clientv1.Response); !r.GetOk() {
			t.Fatalf("discard %+v", r)
		}
		ic = p.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].Slot != 3 || ic.Slots[0].Item == nil || ic.Slots[0].GetItem().GetQuantity() != 3 || ic.Revision != 3 {
			t.Fatalf("after discard %+v, want revision 3", ic)
		}
		p.send(wsconn.Message{ReqID: "d-2", Data: &clientv1.InventoryDiscard{IdempotencyKey: keys["discard-2"], ItemId: potions.GetItemId(), Quantity: 9}})
		if r := p.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != "bad_quantity" {
			t.Fatalf("over-discard %+v", r)
		}
		if rows, quantity, _ := s.itemTotals(t, id, "red_potion"); rows != 1 || quantity != 3 {
			t.Fatalf("%d rows holding %d potions", rows, quantity)
		}
		s.checkInvariants(t)
	})
}

func TestCrashDuringPickups(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs every service as its own process")
	}
	s, procs := processStack(t)
	itemProc, worldProc := procs["item"], procs["world"]

	names := [][2]string{{"pat", "Pat"}, {"quinn", "Quinn"}, {"rae", "Rae"}}
	var hunters []*looter
	for k, n := range names {
		token, id := s.createCharacter(t, n[0], n[1], "warrior")
		s.place(t, id, "forest", 300+float64(k)*80, 608, 204, 0)
		p, snap := s.join(t, token, id)
		p.face(true)
		hunters = append(hunters, newLooter(p, snap))
	}
	var wg sync.WaitGroup
	picked := make([]int, len(hunters))
	for k, h := range hunters {
		wg.Go(func() { picked[k] = h.loot(25 * time.Second) })
	}
	// Hard-kill the world and the item service while pickups are in flight; the gateway stays up.
	time.Sleep(12 * time.Second)
	worldProc.kill()
	itemProc.kill()
	wg.Wait()
	t.Logf("pickups before the kill: %v", picked)
	s.checkInvariants(t)

	itemProc.start()
	worldProc.start()
	for _, h := range hunters {
		if !h.pump(20*time.Second, func(m wsconn.Message) bool { return m.Type == "Snapshot" }) {
			t.Fatalf("%s got no Snapshot after the world came back (closed: %v)", h.id, h.closed(0))
		}
	}

	s.checkInvariants(t)
	for k, n := range names {
		_, w := s.welcome(t, n[0], hunters[k].id)
		held := map[string]int{}
		for _, it := range w.Inventory {
			held[it.GetTemplate()] += int(it.GetQuantity())
		}
		if want := s.inventoryTotals(t, hunters[k].id); len(held) != len(want) || func() bool {
			for tmpl, q := range want {
				if held[tmpl] != q {
					return true
				}
			}
			return false
		}() {
			t.Fatalf("%s is welcomed with %v, the database holds %v", n[1], held, want)
		}
	}
	for _, p := range procs {
		if out := p.out.String(); strings.Contains(out, `"level":"ERROR"`) {
			t.Fatalf("%s logged errors:\n%s", p.name, out)
		}
	}
}
