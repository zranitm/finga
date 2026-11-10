package e2e_test

import (
	"bytes"
	"context"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

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
	inventory int                           // InventoryChanged messages seen
	seen      map[uint32]*clientv1.DropView // every drop seen on this map, gone or not
	collected map[uint32]bool               // drops seen leave for an inventory
	removed   map[uint32]bool               // drops seen leave without an owner taking them
	loots     []*clientv1.Loot
	failures  []*clientv1.Response
	seq       uint32

	// Recorded for the social rows, consumed by their helpers.
	responses   []reply
	partyStates []*clientv1.PartyState
	invites     []*clientv1.PartyInvited
	chatLines   []*clientv1.ChatMessage
	exp         []*clientv1.ExpGained
}

func newLooter(p *player, snap *clientv1.Snapshot) *looter {
	l := &looter{player: p, drops: map[uint32]*clientv1.DropView{}, seen: map[uint32]*clientv1.DropView{}, collected: map[uint32]bool{}, removed: map[uint32]bool{}, seq: 2000}
	for _, d := range snap.Drops {
		l.drops[d.GetId()], l.seen[d.GetId()] = d, d
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
			l.drops[dv.GetId()], l.seen[dv.GetId()] = dv, dv
		}
		for _, id := range d.RemovedDrops {
			l.removed[id] = true
			delete(l.drops, id)
		}
		for _, id := range d.CollectedDrops {
			l.collected[id] = true
			delete(l.drops, id)
		}
	case *clientv1.Snapshot:
		l.drops, l.seen = map[uint32]*clientv1.DropView{}, map[uint32]*clientv1.DropView{} // another map, whose drop IDs are its own
		for _, dv := range d.Drops {
			l.drops[dv.GetId()], l.seen[dv.GetId()] = dv, dv
		}
		for _, v := range d.Players {
			if v.GetId() == l.id {
				l.x, l.y, l.hp = v.X, v.Y, v.GetHp()
			}
		}
	case *clientv1.InventoryChanged:
		l.inventory++
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
	case *clientv1.Loot:
		l.loots = append(l.loots, d)
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

// farm attacks every 850 ms for d, its drops flying to it on their own; it returns the InventoryChanged seen.
func (l *looter) farm(d time.Duration) int {
	deadline := time.Now().Add(d)
	nextAttack := time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextAttack) {
			if l.attack() != nil {
				return l.inventory
			}
			nextAttack = time.Now().Add(850 * time.Millisecond)
		}
		if !l.pump(50*time.Millisecond, nil) && l.closed(0) {
			return l.inventory
		}
	}
	return l.inventory
}

// huntDrops attacks every 850 ms until n drops have been seen on the map, or d passes.
func (l *looter) huntDrops(d time.Duration, n int) bool {
	deadline := time.Now().Add(d)
	next := time.Now()
	for time.Now().Before(deadline) && len(l.seen) < n {
		if !time.Now().Before(next) {
			if err := l.attack(); err != nil {
				return false
			}
			next = time.Now().Add(850 * time.Millisecond)
		}
		l.pump(100*time.Millisecond, nil)
	}
	return len(l.seen) >= n
}

// lootOf is what this looter heard became of a drop, of kind.
func (l *looter) lootOf(drop uint32, kind clientv1.LootKind) []*clientv1.Loot {
	var out []*clientv1.Loot
	for _, lt := range l.loots {
		if lt.GetDropId() == drop && lt.GetKind() == kind {
			out = append(out, lt)
		}
	}
	return out
}

// dataEdit replaces old with new in a game data file, or writes the whole file when old is empty.
type dataEdit struct{ file, old, new string }

// gameData is a copy of the game data with the edits made; a file that no longer holds what an edit replaces fails the test.
func gameData(t *testing.T, edits ...dataEdit) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../data")); err != nil {
		t.Fatal(err)
	}
	for _, e := range edits {
		path := filepath.Join(dir, e.file)
		b := []byte(e.new)
		if e.old != "" {
			was, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(was, []byte(e.old)) {
				t.Fatalf("%s no longer holds what this test rewrites: %q", e.file, e.old)
			}
			b = bytes.Replace(was, []byte(e.old), b, 1)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// slimesDrop has a green slime drop rows, in mobs.yaml's form, instead of its gel and potion.
func slimesDrop(rows string) dataEdit {
	return dataEdit{"mobs.yaml", "      - {item: slime_gel, chance: 0.6, min: 1, max: 2}\n      - {item: red_potion, chance: 0.15, min: 1, max: 1}\n", rows}
}

// lootTimes sets loot.yaml's three timings.
func lootTimes(collectDelay, dropLife, roomCheck string) dataEdit {
	return dataEdit{file: "loot.yaml", new: "collect_delay: " + collectDelay + "\ndrop_life: " + dropLife + "\nroom_check_every: " + roomCheck + "\n"}
}

const (
	oneGel = "      - {item: slime_gel, chance: 1, min: 1, max: 1}\n"
	oneCap = "      - {item: leather_cap, chance: 1, min: 1, max: 1}\n"
)

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

// swapper gives the character a second sword, then returns a loop that wears its two swords in turn through the item service for d, counting the swaps that answered.
func (s *stack) swapper(t *testing.T, id string) func(d time.Duration) int {
	t.Helper()
	ctx := context.Background()
	actor := s.actor(t, id)
	s.give(t, id, "wooden_sword", &itemv1.ItemStats{PhysicalAttack: 10})
	rows, err := s.db.Query(ctx, `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'wooden_sword'`, id)
	if err != nil {
		t.Fatal(err)
	}
	swords, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(swords) != 2 {
		t.Fatalf("swords %v (%v), want the starter and the second", swords, err)
	}
	return func(d time.Duration) int {
		n := 0
		for k, end := 0, time.Now().Add(d); time.Now().Before(end); k++ {
			callCtx, cancel := context.WithTimeout(ctx, time.Second)
			_, err := s.items.Equip(callCtx, &itemv1.EquipRequest{Actor: actor, IdempotencyKey: uuidv7.NewString(), ItemId: swords[k%2], Level: 1, Class: "warrior"})
			cancel()
			if err == nil {
				n++
			}
			time.Sleep(20 * time.Millisecond)
		}
		return n
	}
}

func (s *stack) checkInvariants(t *testing.T) {
	t.Helper()
	data, err := gamedata.Load(s.data)
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

func TestLootAndKeep(t *testing.T) {
	t.Run("D1-D2/a-kill-drops-an-item-everyone-sees-and-it-flies-to-its-killer-as-one-item", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "liam", "Liam", "warrior")
		s.place(t, id, "forest", 380, 608, 184, 0)
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

		// No key: the drop flies to its killer within a few seconds, one item per drop.
		if !killer.until(10*time.Second, func() bool { return killer.inventory > 0 }) {
			t.Fatalf("no InventoryChanged within 10 s of the kill (failures %+v)", killer.failures)
		}
		killer.pump(2*time.Second, nil) // the kill's other drops, if any
		if len(killer.failures) != 0 {
			t.Fatalf("failures %+v", killer.failures)
		}
		if !killer.collected[drop.GetId()] || !slices.ContainsFunc(killer.loots, func(l *clientv1.Loot) bool {
			return l.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && l.GetDropId() == drop.GetId() && l.GetOwnerId() == id && l.GetOwnerName() == "Liam"
		}) {
			t.Fatalf("collected %v, loot %v; want drop %d collected for Liam", killer.collected, killer.loots, drop.GetId())
		}
		if drop.GetOwnerId() != id {
			t.Fatalf("the drop shows owner %q, want its killer", drop.GetOwnerId())
		}
		if !watcher.until(3*time.Second, func() bool { return watcher.collected[drop.GetId()] }) || len(watcher.loots) != 0 {
			t.Fatalf("the watcher saw collected %v and loot %v; want the drop collected, and no loot of a stranger's", watcher.collected, watcher.loots)
		}
		var lootEvents, distinctDrops int
		if err := s.db.QueryRow(context.Background(), `
			SELECT count(*), count(DISTINCT e.cause_id) FROM item_events e JOIN items i ON i.id = e.item_id
			WHERE i.owner_id = $1 AND e.cause_type = 'drop'`, id).Scan(&lootEvents, &distinctDrops); err != nil {
			t.Fatal(err)
		}
		if lootEvents == 0 || lootEvents != distinctDrops {
			t.Fatalf("%d loot events under %d drop IDs; want one per drop", lootEvents, distinctDrops)
		}
		if rows, _, _ := s.itemTotals(t, id, drop.Template); rows != 1 {
			t.Fatalf("%s: %d rows, want the one stack", drop.Template, rows)
		}
		wantQty := s.inventoryTotals(t, id)[drop.Template]
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

	t.Run("D5/a-grant-retried-under-its-key-gives-one-item-whatever-the-batch", func(t *testing.T) {
		// The world's exact item-service call, made again with the drop's key regrouped beside a new drop, as a retry after a lost reply would.
		s := newStack(t)
		_, id := s.createCharacter(t, "noah", "Noah", "warrior")
		actor := s.actor(t, id)
		drop := func(key string) *itemv1.AddManyDrop {
			return &itemv1.AddManyDrop{IdempotencyKey: key, TemplateId: "slime_gel", Quantity: 2, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: key}}
		}
		key, other := uuidv7.NewString(), uuidv7.NewString()
		first, err := s.items.AddMany(context.Background(), &itemv1.AddManyRequest{Actor: actor, Drops: []*itemv1.AddManyDrop{drop(key)}})
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.items.AddMany(context.Background(), &itemv1.AddManyRequest{Actor: actor, Drops: []*itemv1.AddManyDrop{drop(other), drop(key)}})
		if err != nil {
			t.Fatal(err)
		}
		if g := first.GetOutcomes()[0].GetGrant(); g != itemv1.Grant_GRANT_GRANTED {
			t.Fatalf("the first try: %v", g)
		}
		replay := again.GetOutcomes()[1]
		if again.GetOutcomes()[0].GetGrant() != itemv1.Grant_GRANT_GRANTED || replay.GetGrant() != itemv1.Grant_GRANT_ALREADY_GRANTED ||
			replay.GetRevision() != first.GetRevision() || replay.GetChanged()[0].GetItem().GetId() != first.GetChanged()[0].GetItem().GetId() {
			t.Fatalf("the retry answered %+v; want the new drop granted and the old one its stored reply", again.GetOutcomes())
		}
		if rows, quantity, events := s.itemTotals(t, id, "slime_gel"); rows != 1 || quantity != 4 || events != 2 {
			t.Fatalf("%d rows holding %d with %d events; want one stack of the two drops, one event each", rows, quantity, events)
		}
		s.checkInvariants(t)
	})

	t.Run("E3/a-party-takes-its-drops-in-turn-and-a-member-who-died-gets-none", func(t *testing.T) {
		s := newStackOver(t, gameData(t, slimesDrop(oneGel)))
		var ls []*looter
		for _, n := range []struct {
			user, name string
			x          float64
			hp         int
		}{{"pia", "Pia", 380, 184}, {"ravi", "Ravi", 120, 8}} { // Ravi waits left of the slime field, weak enough to die in it later
			token, id := s.createCharacter(t, n.user, n.name, "warrior")
			s.place(t, id, "forest", n.x, 608, n.hp, 0)
			p, snap := s.join(t, token, id)
			ls = append(ls, newLooter(p, snap))
		}
		pia, ravi := ls[0], ls[1]
		if r := pia.command(t, "inv", &clientv1.PartyInvite{CharacterId: ravi.id}); !r.GetOk() {
			t.Fatalf("invite: %+v", r)
		}
		inv := take(t, ravi, "PartyInvited", &ravi.invites, 5*time.Second, nil)
		if r := ravi.command(t, "acc", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
			t.Fatalf("accept: %+v", r)
		}
		pia.partyState(t, 2)
		ravi.partyState(t, 2) // the world's copy of the party follows the social service's stream
		time.Sleep(500 * time.Millisecond)
		pia.face(true)

		// Pia kills; her drops go in turn, from the first member in join order: Pia, Ravi, Pia...
		if !pia.huntDrops(90*time.Second, 2) {
			t.Fatalf("%d drops in 90 s of hunting, want 2", len(pia.seen))
		}
		pia.pump(3*time.Second, nil)
		ravi.pump(time.Second, nil)
		ids := slices.Sorted(maps.Keys(pia.seen))
		owned := map[string]int{}
		for k, id := range ids {
			want := []string{pia.id, ravi.id}[k%2]
			if got := pia.seen[id].GetOwnerId(); got != want {
				t.Fatalf("drop %d of %v is %s's, want %s's: the turn", id, ids, got, want)
			}
			owned[want]++
			for _, l := range ls { // each member on the map hears each one collected, naming its owner
				if got := l.lootOf(id, clientv1.LootKind_LOOT_KIND_COLLECTED); len(got) != 1 || got[0].GetOwnerId() != want {
					t.Fatalf("%s heard %v of drop %d, want it collected for %s", l.id, got, id, want)
				}
			}
		}
		for _, l := range ls {
			if _, quantity, _ := s.itemTotals(t, l.id, "slime_gel"); quantity != owned[l.id] {
				t.Fatalf("%s holds %d gel, want the %d drops that were theirs", l.id, quantity, owned[l.id])
			}
		}

		// Ravi dies, and respawns in the town: neither dead nor on another map does he get a turn.
		ravi.dieAmongSlimes(t)
		before := len(pia.seen)
		if !pia.huntDrops(90*time.Second, before+2) {
			t.Fatalf("no new drops in 90 s of hunting after Ravi died")
		}
		for _, id := range slices.Sorted(maps.Keys(pia.seen))[before:] {
			if got := pia.seen[id].GetOwnerId(); got != pia.id {
				t.Fatalf("drop %d went to %s with Ravi dead or gone", id, got)
			}
		}
		s.checkInvariants(t)
	})

	t.Run("D1/a-full-inventory-keeps-a-drop-waiting-until-there-is-room-or-its-life-ends", func(t *testing.T) {
		s := newStackOver(t, gameData(t, slimesDrop(oneCap), lootTimes("1s", "6s", "1s")))
		token, id := s.createCharacter(t, "sol", "Sol", "warrior")
		s.place(t, id, "forest", 380, 608, 184, 0)
		for range 24 {
			s.give(t, id, "leather_cap", &itemv1.ItemStats{PhysicalDefense: 3})
		}
		l, _ := s.hello(t, token, id)
		l.face(true)
		first, ok := l.hunt(90*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap dropped in 90 s of hunting")
		}
		if !l.until(5*time.Second, func() bool { return len(l.lootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_WAITING)) == 1 }) {
			t.Fatalf("loot %v; want the cap waiting for room", l.loots)
		}
		if _, still := l.drops[first.GetId()]; !still || l.inventory != 0 {
			t.Fatalf("the cap left the ground, or something reached the full Equip tab (%d changes)", l.inventory)
		}

		// A discard makes room: the waiting cap arrives within a room check.
		var held string
		if err := s.db.QueryRow(context.Background(), `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' LIMIT 1`, id).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if r := l.command(t, "d-1", &clientv1.InventoryDiscard{IdempotencyKey: key(), ItemId: held, Quantity: 1}); !r.GetOk() {
			t.Fatalf("discard: %+v", r)
		}
		// A tick's frame goes before its owner's events: wait for the Loot, which follows collected_drops.
		if !l.until(3*time.Second, func() bool {
			return l.collected[first.GetId()] && len(l.lootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)) == 1
		}) {
			t.Fatalf("collected %v, loot %v; want the cap collected once there was room", l.collected, l.loots)
		}

		// The tab is full again: the next cap waits out its life and is lost, never collected.
		next, ok := l.hunt(90*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no second cap in 90 s of hunting")
		}
		if !l.until(10*time.Second, func() bool {
			return l.removed[next.GetId()] && len(l.lootOf(next.GetId(), clientv1.LootKind_LOOT_KIND_LOST)) == 1
		}) {
			t.Fatalf("the second cap never left the ground as lost (loot %v)", l.loots)
		}
		if l.collected[next.GetId()] {
			t.Fatalf("loot %v; want the second cap lost, and not collected", l.loots)
		}
		var caps int
		if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' AND location = 'inventory'`, id).Scan(&caps); err != nil {
			t.Fatal(err)
		}
		if caps != 24 {
			t.Fatalf("%d caps held, want the 24: one discarded, one collected", caps)
		}
		s.checkInvariants(t)
	})

	t.Run("D1/an-owner-who-takes-a-portal-before-collection-still-gets-the-drop", func(t *testing.T) {
		s := newStackOver(t, gameData(t, slimesDrop(oneGel), lootTimes("8s", "60s", "2s")))
		token, id := s.createCharacter(t, "uma", "Uma", "warrior")
		s.place(t, id, "forest", 300, 608, 184, 0)
		p, snap := s.join(t, token, id)
		l := newLooter(p, snap)
		l.face(true)
		gel, ok := l.hunt(90*time.Second, "slime_gel")
		if !ok {
			t.Fatal("no gel in 90 s of hunting")
		}
		l.hop(t, 64, "town", 1216) // the forest's to_town portal, within the 8 s before the gel flies
		if !l.until(12*time.Second, func() bool {
			return l.inventory > 0 && len(l.lootOf(gel.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)) == 1 // the Loot follows its InventoryChanged
		}) {
			t.Fatalf("no InventoryChanged and Loot in the town (loot %v)", l.loots)
		}
		got := l.lootOf(gel.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)
		if len(got) != 1 || got[0].GetMapId() != "forest" || got[0].GetOwnerId() != id {
			t.Fatalf("loot %v; want the forest's gel collected for Uma", l.loots)
		}
		if _, quantity, _ := s.itemTotals(t, id, "slime_gel"); quantity < 1 {
			t.Fatal("the gel is not in the inventory")
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
		s.place(t, id, "forest", 300+float64(k)*80, 608, 184, 0)
		p, snap := s.join(t, token, id)
		p.face(true)
		hunters = append(hunters, newLooter(p, snap))
	}
	var wg sync.WaitGroup
	collected, swapped := make([]int, len(hunters)), make([]int, len(hunters))
	for k, h := range hunters {
		swap := s.swapper(t, h.id)
		wg.Go(func() { collected[k] = h.farm(25 * time.Second) })
		wg.Go(func() { swapped[k] = swap(25 * time.Second) })
	}
	// Hard-kill the world and the item service while grants and swaps are in flight; the gateway stays up.
	time.Sleep(12 * time.Second)
	worldProc.kill()
	itemProc.kill()
	wg.Wait()
	t.Logf("inventory changes before the kill: %v; swaps: %v", collected, swapped)
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
