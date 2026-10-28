package e2e_test

import (
	"context"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/item/invariants"
)

// looter wraps a player with what the loot rows need to know about the
// world: its own position and the drops on the ground, kept current from
// every message it reads.
type looter struct {
	*player
	x, y      float64
	hp        int
	drops     map[uint64]protocol.DropView
	inventory int // InventoryChanged messages seen
	// picked are the drops this looter's pickups took off the ground: the
	// server removes a picked drop in the Delta of the tick whose
	// InventoryChanged reports it, so a drop removed in a tick that brought
	// this looter an InventoryChanged was picked by it. A drop that expired
	// in another tick is not in it.
	picked      map[uint64]protocol.DropView
	removedTick uint64
	removed     []protocol.DropView // the drops the Delta of removedTick took away
	failures    []protocol.Response
	seq         uint32

	// Recorded for the social rows, consumed by their helpers.
	responses   []reply
	partyStates []protocol.PartyState
	invites     []protocol.PartyInvited
	chatLines   []protocol.ChatMessage
	exp         []protocol.ExpGained
}

func newLooter(p *player, snap protocol.Snapshot) *looter {
	l := &looter{player: p, drops: map[uint64]protocol.DropView{}, picked: map[uint64]protocol.DropView{}, seq: 2000}
	for _, d := range snap.Drops {
		l.drops[d.ID] = d
	}
	for _, v := range snap.Players {
		if v.ID == p.id {
			l.x, l.y, l.hp = v.X, v.Y, v.HP
		}
	}
	return l
}

// observe updates the looter's view of the world from one message.
func (l *looter) observe(m wsconn.Message) {
	switch d := m.Data.(type) {
	case protocol.Delta:
		for _, v := range d.Players {
			if v.ID == l.id {
				l.x, l.y, l.hp = v.X, v.Y, v.HP
			}
		}
		for _, dv := range d.Drops {
			l.drops[dv.ID] = dv
		}
		l.removedTick, l.removed = m.Tick, l.removed[:0]
		for _, id := range d.RemovedDrops {
			if dv, ok := l.drops[id]; ok {
				l.removed = append(l.removed, dv)
			}
			delete(l.drops, id)
		}
	case protocol.Snapshot:
		l.drops = map[uint64]protocol.DropView{}
		for _, dv := range d.Drops {
			l.drops[dv.ID] = dv
		}
		for _, v := range d.Players {
			if v.ID == l.id {
				l.x, l.y, l.hp = v.X, v.Y, v.HP
			}
		}
	case protocol.InventoryChanged:
		l.inventory++
		if m.Tick == l.removedTick {
			for _, dv := range l.removed {
				l.picked[dv.ID] = dv
			}
		}
	case protocol.Response:
		if !d.OK {
			l.failures = append(l.failures, d)
		}
		l.responses = append(l.responses, reply{m.ReqID, d})
	case protocol.PartyState:
		l.partyStates = append(l.partyStates, d)
	case protocol.PartyInvited:
		l.invites = append(l.invites, d)
	case protocol.ChatMessage:
		l.chatLines = append(l.chatLines, d)
	case protocol.ExpGained:
		l.exp = append(l.exp, d)
	}
}

type reply struct {
	req string
	r   protocol.Response
}

// until pumps in short slices until pick finds what it wants in the
// recorded messages (and consumes it), for up to d.
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

// pump reads messages for up to d, observing each, until pred is true.
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
	return l.conn.Write(context.Background(), websocket.MessageText, b)
}

func (l *looter) input(in protocol.Input) error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: in})
}

func (l *looter) attack() error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: protocol.Attack{}})
}

func (l *looter) pickUp() error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: protocol.PickUp{}})
}

// nearestDrop is the closest drop of the template ("" for any).
func (l *looter) nearestDrop(template string) (protocol.DropView, bool) {
	var best protocol.DropView
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
func (l *looter) hunt(d time.Duration, template string) (protocol.DropView, bool) {
	deadline := time.Now().Add(d)
	next := time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(next) {
			if err := l.attack(); err != nil {
				return protocol.DropView{}, false
			}
			next = time.Now().Add(850 * time.Millisecond)
		}
		l.pump(100*time.Millisecond, nil)
		if drop, ok := l.nearestDrop(template); ok {
			return drop, true
		}
	}
	return protocol.DropView{}, false
}

// walkTo walks until the character stands within 12 px of x, then stops.
// Contact with a mob knocks the character back, so the direction is
// re-evaluated every tick; a character that stops making progress (a well,
// a boulder) jumps.
func (l *looter) walkTo(x float64, d time.Duration) bool {
	deadline := time.Now().Add(d)
	lastX, stuck := l.x, 0
	for time.Now().Before(deadline) {
		dx := x - l.x
		if math.Abs(dx) < 12 {
			_ = l.input(protocol.Input{})
			l.pump(100*time.Millisecond, nil)
			return math.Abs(x-l.x) < 24
		}
		if math.Abs(l.x-lastX) < 1 {
			stuck++
		} else {
			stuck = 0
		}
		lastX = l.x
		if err := l.input(protocol.Input{Right: dx > 0, Left: dx < 0, Jump: stuck >= 3}); err != nil {
			return false
		}
		l.pump(50*time.Millisecond, nil)
	}
	_ = l.input(protocol.Input{})
	return false
}

// loot hunts, walks to drops and picks them up until the deadline or the
// connection dies; it reports how many pickups succeeded.
func (l *looter) loot(d time.Duration) int {
	deadline := time.Now().Add(d)
	nextAttack := time.Now()
	for time.Now().Before(deadline) {
		if drop, ok := l.nearestDrop(""); ok {
			if math.Abs(drop.X-l.x) >= 12 {
				if l.input(protocol.Input{Right: drop.X > l.x, Left: drop.X < l.x}) != nil {
					return l.inventory
				}
			} else {
				_ = l.input(protocol.Input{})
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
	_ = l.input(protocol.Input{})
	return l.inventory
}

// welcome says Hello with a fresh login and returns the Welcome message,
// which join discards. The old session of the account is replaced.
func (s *stack) welcome(t *testing.T, username, id string) (*player, protocol.Welcome) {
	t.Helper()
	token := s.call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).body["token"].(string)
	p := s.connect(t, id)
	p.send(wsconn.Message{Data: protocol.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterID: id}})
	w := p.expect("Welcome").Data.(protocol.Welcome)
	p.token = w.Token
	p.expect("Snapshot")
	return p, w
}

// itemTotals is what the database holds for one owner and template: rows,
// total quantity, and item events.
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

// inventoryTotals is every template an owner holds, from the database.
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
	mismatches, err := invariants.CheckInvariants(context.Background(), s.db)
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

		// D1: the drop lands on the ground beside the slime, inside its patrol.
		drop, ok := killer.hunt(90*time.Second, "")
		if !ok {
			t.Fatal("no drop in 90 s of hunting")
		}
		if drop.Y != 608 || drop.X < 150 || drop.X > 850 || drop.Quantity < 1 || (drop.Template != "slime_gel" && drop.Template != "red_potion") {
			t.Fatalf("drop %+v", drop)
		}
		// D1: the other player sees the same drop.
		if !watcher.pump(3*time.Second, func(wsconn.Message) bool { _, seen := watcher.drops[drop.ID]; return seen }) {
			t.Fatalf("the watcher never saw drop %d", drop.ID)
		}

		// D2: walk to it and pick it up twice in a row (two messages in one
		// tick). The second PickUp finds the drop reserved and does nothing,
		// unless a second drop lies in range too (a slime drops two items):
		// then it takes that one, and the rule under test is still "one
		// item per drop". The drop lies where the slimes patrol, and a touch
		// knocks the character back and up, out of range; so as a real
		// client would, the pair is sent again when the character was
		// bounced away before anything arrived.
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
		if _, picked := killer.picked[drop.ID]; !picked || len(killer.picked) != killer.inventory || len(killer.failures) != 0 {
			t.Fatalf("picked %v with %d InventoryChanged, failures %+v; want drop %d among them, one InventoryChanged each", killer.picked, killer.inventory, killer.failures, drop.ID)
		}
		if _, still := killer.drops[drop.ID]; still {
			t.Fatal("the drop is still on the ground")
		}
		if !watcher.pump(3*time.Second, func(wsconn.Message) bool { _, seen := watcher.drops[drop.ID]; return !seen }) {
			t.Fatal("the watcher still sees the drop")
		}
		// D5: the path a real client takes after a lost reply: it re-sends
		// the same PickUp (same seq). PickUp has no request ID, so the re-send
		// is a new pickup: it takes nothing already taken, and if another
		// drop now lies in range (the second item of a slime, or one the
		// character was knocked next to) it rightly takes that one. Either
		// way every InventoryChanged is one drop leaving the ground.
		if err := killer.trySend(wsconn.Message{Seq: killer.seq, Data: protocol.PickUp{}}); err != nil {
			t.Fatal(err)
		}
		killer.pump(time.Second, nil)
		if len(killer.picked) != killer.inventory || len(killer.failures) != 0 {
			t.Fatalf("after re-sending PickUp with seq %d: %d InventoryChanged for %d drops picked, failures %+v", killer.seq, killer.inventory, len(killer.picked), killer.failures)
		}
		taken := len(killer.picked)
		// One item event per drop, each under its own drop ID, and the
		// template's stack holds the starter stack plus the drop.
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
				wantQty += d.Quantity
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

		// D2: the item is there after a reconnect.
		_, w := s.welcome(t, "liam", id)
		held := 0
		for _, it := range w.Inventory {
			if it.Template == drop.Template {
				held += it.Quantity
			}
		}
		if held != wantQty {
			t.Fatalf("welcome inventory holds %d %s, want %d: %+v", held, drop.Template, wantQty, w.Inventory)
		}
		s.checkInvariants(t)
	})

	t.Run("D5/a-retried-pickup-job-gives-one-item", func(t *testing.T) {
		// The worker's exact call to the item service, twice with the drop's
		// key, as a retry after a lost reply would make it.
		s := newStack(t)
		_, id := s.createCharacter(t, "noah", "Noah", "warrior")
		var account string
		if err := s.db.QueryRow(context.Background(), `SELECT account_id FROM characters WHERE id = $1`, id).Scan(&account); err != nil {
			t.Fatal(err)
		}
		key := uuid.New().String()
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
		var potions protocol.ItemView
		for _, it := range welcome.Inventory {
			if it.Template == "red_potion" {
				potions = it
			}
		}
		if potions.ItemID == "" || potions.Quantity != 5 || potions.Tab != "use" || potions.Slot != 0 {
			t.Fatalf("starter potions %+v in %+v", potions, welcome.Inventory)
		}

		// Move to slot 3: ok, two changed slots.
		p.send(wsconn.Message{ReqID: "m-1", Data: protocol.InventoryMove{IdempotencyKey: "move-1", ItemID: potions.ItemID, Tab: "use", Slot: 3}})
		r := p.expect("Response")
		if r.ReqID != "m-1" || !r.Data.(protocol.Response).OK {
			t.Fatalf("move response %+v", r)
		}
		ic := p.expect("InventoryChanged").Data.(protocol.InventoryChanged)
		if len(ic.Slots) != 2 || ic.Slots[0].Slot != 0 || ic.Slots[0].Item != nil || ic.Slots[1].Slot != 3 || ic.Slots[1].Item.Quantity != 5 {
			t.Fatalf("after move %+v", ic)
		}
		// The same key again is the same answer; nothing moves twice.
		p.send(wsconn.Message{ReqID: "m-2", Data: protocol.InventoryMove{IdempotencyKey: "move-1", ItemID: potions.ItemID, Tab: "use", Slot: 3}})
		if r := p.expect("Response"); r.ReqID != "m-2" || !r.Data.(protocol.Response).OK {
			t.Fatalf("replayed move %+v", r)
		}
		p.expect("InventoryChanged")
		// A move across tabs is refused.
		p.send(wsconn.Message{ReqID: "m-3", Data: protocol.InventoryMove{IdempotencyKey: "move-3", ItemID: potions.ItemID, Tab: "etc", Slot: 1}})
		if r := p.expect("Response").Data.(protocol.Response); r.OK || r.Code != "wrong_tab" {
			t.Fatalf("cross-tab move %+v", r)
		}
		// Discard two, then more than there are.
		p.send(wsconn.Message{ReqID: "d-1", Data: protocol.InventoryDiscard{IdempotencyKey: "discard-1", ItemID: potions.ItemID, Quantity: 2}})
		if r := p.expect("Response").Data.(protocol.Response); !r.OK {
			t.Fatalf("discard %+v", r)
		}
		ic = p.expect("InventoryChanged").Data.(protocol.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].Slot != 3 || ic.Slots[0].Item == nil || ic.Slots[0].Item.Quantity != 3 {
			t.Fatalf("after discard %+v", ic)
		}
		p.send(wsconn.Message{ReqID: "d-2", Data: protocol.InventoryDiscard{IdempotencyKey: "discard-2", ItemID: potions.ItemID, Quantity: 9}})
		if r := p.expect("Response").Data.(protocol.Response); r.OK || r.Code != "bad_quantity" {
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
		t.Skip("builds and runs the item service and the game server")
	}
	bins := buildBinaries(t)
	dbURL := cluster.URL(t)
	itemProc := startServiceProcess(t, "item", bins["item"], dbURL)
	accountProc := startServiceProcess(t, "account", bins["account"], dbURL)
	socialProc := startServiceProcess(t, "social", bins["social"], dbURL)
	proc := startGameserver(t, bins["gameserver"], dbURL, freePort(t), map[string]*process{"item": itemProc, "account": accountProc, "social": socialProc})
	db, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	s := &stack{t: t, url: proc.url, db: db}

	// Three hunters in the slime field, looting as fast as they can.
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
		wg.Add(1)
		go func() { defer wg.Done(); picked[k] = h.loot(25 * time.Second) }()
	}
	// Hard kill in the middle of it, while pickups are in flight: the game
	// server (the world's workers mid-call) and the item service (its
	// transactions mid-commit), one right after the other.
	time.Sleep(12 * time.Second)
	proc.kill()
	itemProc.kill()
	wg.Wait()
	for _, h := range hunters {
		if !h.closed(5 * time.Second) {
			t.Fatal("a connection survived the kill")
		}
	}
	t.Logf("pickups before the kill: %v", picked)

	// The database is consistent on its own and after a restart, and each
	// hunter is welcomed back with exactly what the database holds.
	s.checkInvariants(t)
	itemProc.start()
	proc.start()
	s.checkInvariants(t)
	for k, n := range names {
		_, w := s.welcome(t, n[0], hunters[k].id)
		held := map[string]int{}
		for _, it := range w.Inventory {
			held[it.Template] += it.Quantity
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
	for _, p := range []*process{proc, itemProc, accountProc, socialProc} {
		if out := p.out.String(); strings.Contains(out, `"level":"ERROR"`) {
			t.Fatalf("%s logged errors:\n%s", p.name, out)
		}
	}
}
