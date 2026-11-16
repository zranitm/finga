package e2etest

import (
	"bytes"
	"context"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/repository/item/itemtest"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// Looter is a player tracking its position and the drops on the ground from every message.
type Looter struct {
	*Player
	X, Y, Z   float64
	HP        int32
	heading   *clientv1.Input // the walk chase last sent
	Mobs      map[uint32]*clientv1.MobView
	Drops     map[uint32]*clientv1.DropView
	Inventory int                           // InventoryChanged messages seen
	Seen      map[uint32]*clientv1.DropView // every drop seen on this map, gone or not
	Collected map[uint32]bool               // drops seen leave for an inventory
	Removed   map[uint32]bool               // drops seen leave without an owner taking them
	Loots     []*clientv1.Loot
	Failures  []*clientv1.Response
	// The player's own view in the last frame, and the newest input the world says it applied.
	onGround   bool
	vx, vy, vz float64
	ack        uint32
	tick       uint64 // the newest frame's, in this map's ticks

	// Recorded for the social rows, consumed by their helpers.
	Responses   []Reply
	PartyStates []*clientv1.PartyState
	Invites     []*clientv1.PartyInvited
	ChatLines   []*clientv1.ChatMessage
	Exp         []*clientv1.ExpGained
	Dialogs     []*clientv1.NpcDialog
	Quests      []*clientv1.QuestChanged

	// Recorded for the field boss rows.
	FieldBosses   []*clientv1.FieldBoss
	BossesGone    []*clientv1.FieldBossGone
	Announcements []*clientv1.Announcement
}

// NewLooter follows p from the Snapshot it joined to.
func NewLooter(p *Player, snap *clientv1.Snapshot) *Looter {
	l := &Looter{Player: p, Mobs: map[uint32]*clientv1.MobView{}, Drops: map[uint32]*clientv1.DropView{}, Seen: map[uint32]*clientv1.DropView{}, Collected: map[uint32]bool{}, Removed: map[uint32]bool{}}
	for _, mv := range snap.Mobs {
		l.Mobs[mv.GetId()] = mv
	}
	for _, d := range snap.Drops {
		l.Drops[d.GetId()], l.Seen[d.GetId()] = d, d
	}
	for _, v := range snap.Players {
		l.see(v)
	}
	return l
}

// see takes the player's own view; others' are left alone.
func (l *Looter) see(v *clientv1.PlayerView) {
	if v.GetId() == l.ID {
		l.X, l.Y, l.Z, l.HP = v.X, v.Y, v.Z, v.GetHp()
		l.onGround, l.vx, l.vy, l.vz = v.GetOnGround(), v.Vx, v.Vy, v.Vz
	}
}

// Observe takes in one message: the frames' mobs and drops, and every inventory, reply, party, chat, exp, loot, dialog and quest message, kept in order.
func (l *Looter) Observe(m wsconn.Message) {
	switch d := m.Data.(type) {
	case *clientv1.Delta:
		l.ack, l.tick = d.GetAckSeq(), m.Tick
		for _, v := range d.Players {
			l.see(v)
		}
		for _, mv := range d.Mobs {
			l.Mobs[mv.GetId()] = mv
		}
		for _, id := range d.RemovedMobs {
			delete(l.Mobs, id)
		}
		for _, dv := range d.Drops {
			l.Drops[dv.GetId()], l.Seen[dv.GetId()] = dv, dv
		}
		for _, id := range d.RemovedDrops {
			l.Removed[id] = true
			delete(l.Drops, id)
		}
		for _, id := range d.CollectedDrops {
			l.Collected[id] = true
			delete(l.Drops, id)
		}
	case *clientv1.Snapshot:
		// Another map, whose mob and drop IDs and ticks are its own.
		l.Mobs, l.Drops, l.Seen, l.tick = map[uint32]*clientv1.MobView{}, map[uint32]*clientv1.DropView{}, map[uint32]*clientv1.DropView{}, m.Tick
		for _, mv := range d.Mobs {
			l.Mobs[mv.GetId()] = mv
		}
		for _, dv := range d.Drops {
			l.Drops[dv.GetId()], l.Seen[dv.GetId()] = dv, dv
		}
		for _, v := range d.Players {
			l.see(v)
		}
	case *clientv1.InventoryChanged:
		l.Inventory++
	case *clientv1.Response:
		if !d.GetOk() {
			l.Failures = append(l.Failures, d)
		}
		l.Responses = append(l.Responses, Reply{m.ReqID, d})
	case *clientv1.PartyState:
		l.PartyStates = append(l.PartyStates, d)
	case *clientv1.PartyInvited:
		l.Invites = append(l.Invites, d)
	case *clientv1.ChatMessage:
		l.ChatLines = append(l.ChatLines, d)
	case *clientv1.ExpGained:
		l.Exp = append(l.Exp, d)
	case *clientv1.Loot:
		l.Loots = append(l.Loots, d)
	case *clientv1.NpcDialog:
		l.Dialogs = append(l.Dialogs, d)
	case *clientv1.QuestChanged:
		l.Quests = append(l.Quests, d)
	case *clientv1.FieldBoss:
		l.FieldBosses = append(l.FieldBosses, d)
	case *clientv1.FieldBossGone:
		l.BossesGone = append(l.BossesGone, d)
	case *clientv1.Announcement:
		l.Announcements = append(l.Announcements, d)
	}
}

// Reply is a Response with the request id it answers.
type Reply struct {
	Req string
	R   *clientv1.Response
}

// Until pumps in short slices, for up to d, until pick finds (and consumes) what it wants.
func (l *Looter) Until(d time.Duration, pick func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if pick() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		l.Pump(50*time.Millisecond, nil)
	}
}

// Pump reads and observes messages for up to d, until one meets pred.
func (l *Looter) Pump(d time.Duration, pred func(wsconn.Message) bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		m, ok := l.Read(time.Until(deadline))
		if !ok {
			return false
		}
		l.Observe(m)
		if pred != nil && pred(m) {
			return true
		}
	}
	return false
}

// trySend is send without failing the test, for goroutines.
func (l *Looter) trySend(m wsconn.Message) error {
	b, err := l.codec.Encode(m)
	if err != nil {
		return err
	}
	return l.Conn.Write(context.Background(), websocket.MessageBinary, b)
}

// Input sends one input at the next sequence number.
func (l *Looter) Input(in *clientv1.Input) error {
	return l.trySend(wsconn.Message{Seq: l.NextSeq(), Data: in})
}

// Attack sends a basic attack aimed along aim.
func (l *Looter) Attack(aim float32) error {
	return l.trySend(wsconn.Message{Seq: l.NextSeq(), Data: &clientv1.Attack{Aim: aim}})
}

// Swing attacks the nearest living mob on the looter's level, aimed at it as the client's auto-aim aims.
func (l *Looter) Swing() error {
	return l.Attack(l.AimAt(l.Nearest("")))
}

// Nearest is the nearest living mob of the type ("" for any) within 2 m of the looter's height, by its distance across the ground; nil with none.
func (l *Looter) Nearest(typ string) *clientv1.MobView {
	var best *clientv1.MobView
	bestDist := math.Inf(1)
	for _, m := range l.Mobs {
		if m.GetHp() <= 0 || typ != "" && m.GetType() != typ || math.Abs(m.GetY()-l.Y) >= 2 {
			continue
		}
		if dist := math.Hypot(m.GetX()-l.X, m.GetZ()-l.Z); dist < bestDist {
			best, bestDist = m, dist
		}
	}
	return best
}

// AimAt is the yaw from the looter to m, or north with no mob.
func (l *Looter) AimAt(m *clientv1.MobView) float32 {
	if m == nil {
		return 0
	}
	return yawTo(l.X, l.Z, m.GetX(), m.GetZ())
}

// nearestDrop is the closest drop of the template ("" for any).
func (l *Looter) nearestDrop(template string) (*clientv1.DropView, bool) {
	var best *clientv1.DropView
	bestDist := math.Inf(1)
	for _, d := range l.Drops {
		if template != "" && d.Template != template {
			continue
		}
		if dist := math.Hypot(d.X-l.X, d.Z-l.Z); dist < bestDist {
			best, bestDist = d, dist
		}
	}
	return best, bestDist < math.Inf(1)
}

// Hunt swings every 850 ms until a drop of the template appears.
func (l *Looter) Hunt(d time.Duration, template string) (*clientv1.DropView, bool) {
	deadline := time.Now().Add(d)
	next := time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(next) {
			if err := l.Swing(); err != nil {
				return nil, false
			}
			next = time.Now().Add(850 * time.Millisecond)
		}
		l.Pump(100*time.Millisecond, nil)
		if drop, ok := l.nearestDrop(template); ok {
			return drop, true
		}
	}
	return nil, false
}

// WalkTo walks to within 0.375 m of to and stands there, still on the ground, re-aiming after knockback and jumping when stuck.
func (l *Looter) WalkTo(to gamedata.Point, d time.Duration) bool {
	deadline := time.Now().Add(d)
	lastX, lastZ, stuck := l.X, l.Z, 0
	for time.Now().Before(deadline) {
		dx, dz := to.X-l.X, to.Z-l.Z
		if math.Hypot(dx, dz) < 0.375 {
			if err := l.Input(&clientv1.Input{}); err != nil {
				return false
			}
			l.Pump(100*time.Millisecond, nil)
			if math.Hypot(to.X-l.X, to.Z-l.Z) < 0.75 && l.Standing() {
				return true
			}
			continue // in the air or still moving, as a stuck jump or a lagging frame leaves it, or knocked back while stopping
		}
		if math.Hypot(l.X-lastX, l.Z-lastZ) < 0.03 {
			stuck++
		} else {
			stuck = 0
		}
		lastX, lastZ = l.X, l.Z
		in := Toward(dx, dz)
		in.Jump = stuck >= 3
		if err := l.Input(in); err != nil {
			return false
		}
		l.Pump(50*time.Millisecond, nil)
	}
	_ = l.Input(&clientv1.Input{})
	return false
}

// Farm swings every 850 ms for d, its drops flying to it on their own; it returns the InventoryChanged seen.
func (l *Looter) Farm(d time.Duration) int {
	deadline := time.Now().Add(d)
	nextAttack := time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextAttack) {
			if l.Swing() != nil {
				return l.Inventory
			}
			nextAttack = time.Now().Add(850 * time.Millisecond)
		}
		if !l.Pump(50*time.Millisecond, nil) && l.Closed(0) {
			return l.Inventory
		}
	}
	return l.Inventory
}

// Settle waits until the looter's kills have nothing left to drop or grant and their pushes are read; a drop waiting for room holds it off.
func (l *Looter) Settle(d time.Duration) bool {
	marker := l.NextSeq() // once the world applies it, every attack sent before it is queued or done
	if err := l.trySend(wsconn.Message{Seq: marker, Data: &clientv1.Input{}}); err != nil {
		return false
	}
	var by, emptyAt uint64 // the tick by which a queued attack has struck, and the first from then with none of the looter's drops down
	return l.Until(d, func() bool {
		if by == 0 && l.ack >= marker {
			by = l.tick + 2*l.attackTicks // one length to start behind the action before it, one more to strike
		}
		switch {
		case by == 0 || l.tick < by:
			return false
		case slices.ContainsFunc(slices.Collect(maps.Values(l.Drops)), func(dv *clientv1.DropView) bool { return dv.GetOwnerId() == l.ID }):
			emptyAt = 0
			return false
		case emptyAt == 0:
			emptyAt = l.tick
		}
		return l.tick > emptyAt // a tick's frame goes before its pushes, so the next frame brings the last drop's InventoryChanged and Loot
	})
}

// HuntDrops swings every 850 ms until n drops have been seen on the map, or d passes.
func (l *Looter) HuntDrops(d time.Duration, n int) bool {
	deadline := time.Now().Add(d)
	next := time.Now()
	for time.Now().Before(deadline) && len(l.Seen) < n {
		if !time.Now().Before(next) {
			if err := l.Swing(); err != nil {
				return false
			}
			next = time.Now().Add(850 * time.Millisecond)
		}
		l.Pump(100*time.Millisecond, nil)
	}
	return len(l.Seen) >= n
}

// LootOf is what this looter heard became of a drop, of kind.
func (l *Looter) LootOf(drop uint32, kind clientv1.LootKind) []*clientv1.Loot {
	var out []*clientv1.Loot
	for _, lt := range l.Loots {
		if lt.GetDropId() == drop && lt.GetKind() == kind {
			out = append(out, lt)
		}
	}
	return out
}

// DataEdit replaces Old with New in a game data file, or writes the whole file when Old is empty.
type DataEdit struct{ File, Old, New string }

// dataDir is the server's game data, found from this file, so every package's tests read the same.
var dataDir = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "data")
}()

// GameData is a copy of the game data with the edits made; a file that no longer holds what an edit replaces fails the test.
func GameData(t *testing.T, edits ...DataEdit) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(dataDir)); err != nil {
		t.Fatal(err)
	}
	for _, e := range edits {
		path := filepath.Join(dir, e.File)
		b := []byte(e.New)
		if e.Old != "" {
			was, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(was, []byte(e.Old)) {
				t.Fatalf("%s no longer holds what this test rewrites: %q", e.File, e.Old)
			}
			b = bytes.Replace(was, []byte(e.Old), b, 1)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// QuickSlimes has the forest's ground slimes die to one hit and come back a second later, so a hunt takes a shot or two.
func QuickSlimes() []DataEdit {
	return []DataEdit{
		{"mobs.yaml", "    name: Green Slime\n    level: 1\n    hp: 34\n", "    name: Green Slime\n    level: 1\n    hp: 1\n"},
		{"maps/forest.yaml", "respawn: 8s}", "respawn: 1s}"}, // the slimes' circle
	}
}

// Welcome says Hello with a fresh login, replacing the old session, and returns the Welcome.
func (s *Stack) Welcome(t *testing.T, username, id string) (*Player, *clientv1.Welcome) {
	t.Helper()
	token := s.Call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).Body["token"].(string)
	p := s.Connect(t, id)
	w, _ := p.Hello(token)
	return p, w
}

// ItemTotals are owner's rows of template, their summed quantity, and their events.
func (s *Stack) ItemTotals(t *testing.T, owner, template string) (rows int, quantity int, events int) {
	t.Helper()
	err := s.DB.QueryRow(context.Background(), `
		SELECT count(*), coalesce(sum(quantity), 0),
		       (SELECT count(*) FROM item_events e JOIN items i ON i.id = e.item_id WHERE i.owner_id = $1 AND i.template_id = $2)
		FROM items WHERE owner_id = $1 AND template_id = $2 AND location = 'inventory'`, owner, template).Scan(&rows, &quantity, &events)
	if err != nil {
		t.Fatal(err)
	}
	return rows, quantity, events
}

// InventoryTotals are owner's inventory quantities by template.
func (s *Stack) InventoryTotals(t *testing.T, owner string) map[string]int {
	t.Helper()
	rows, err := s.DB.Query(context.Background(), `SELECT template_id, sum(quantity) FROM items WHERE owner_id = $1 AND location = 'inventory' GROUP BY template_id`, owner)
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

// Swapper gives the character a sword, then returns a loop that wears it and the starter club in turn through the item module for d, counting the swaps that answered.
func (s *Stack) Swapper(t *testing.T, id string) func(d time.Duration) int {
	t.Helper()
	ctx := context.Background()
	actor := s.Actor(t, id)
	s.Give(t, id, "wooden_sword", &progression.ItemStats{PhysicalAttack: 10})
	rows, err := s.DB.Query(ctx, `SELECT id::text FROM items WHERE owner_id = $1 AND template_id IN ('wooden_club', 'wooden_sword')`, id)
	if err != nil {
		t.Fatal(err)
	}
	weapons, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(weapons) != 2 {
		t.Fatalf("weapons %v (%v), want the starter club and the sword", weapons, err)
	}
	ids := []kernel.ItemID{MustID(t, kernel.ParseItemID, weapons[0]), MustID(t, kernel.ParseItemID, weapons[1])}
	return func(d time.Duration) int {
		n := 0
		for k, end := 0, time.Now().Add(d); time.Now().Before(end); k++ {
			callCtx, cancel := context.WithTimeout(ctx, time.Second)
			_, err := s.Items.Equip(callCtx, actor, item.EquipRequest{Key: uuidv7.NewString(), ItemID: ids[k%2], Level: 1, Class: "warrior"})
			cancel()
			if err == nil {
				n++
			}
			time.Sleep(20 * time.Millisecond)
		}
		return n
	}
}

// CheckInvariants holds the database to the item module's invariants, over the stack's game data.
func (s *Stack) CheckInvariants(t *testing.T) {
	t.Helper()
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	mismatches, err := itemtest.CheckInvariants(context.Background(), s.DB, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("invariant mismatches: %v", mismatches)
	}
}
