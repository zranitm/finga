package e2etest

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/repository/item/itemtest"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// Looter is a player tracking its position and its own pouches from every message.
type Looter struct {
	*Player
	X, Y, Z   float64
	HP        int32
	heading   *clientv1.Input // the walk chase last sent
	Mobs      map[uint32]*clientv1.MobView
	Pouches   map[uint32]*clientv1.PouchView // its own on the map now
	Inventory int                            // InventoryChanged messages seen
	Seen      map[uint32]*clientv1.PouchView // every pouch of its own seen on this map, gone or not, as last seen
	Gone      map[uint32]bool                // pouches seen go: emptied, or their life ended
	FellAt    map[uint32]uint64              // the tick of the frame that first carried each pouch Seen
	Loots     []*clientv1.Loot
	Failures  []*clientv1.Response
	taking    string    // the request ID of the TakeLoot Farm waits on
	nextTake  time.Time // Farm takes no sooner, as a player's presses come
	takes     int       // TakeLoot requests sent, for their IDs
	// The player's own view in the last frame, and the newest input the world says it applied.
	onGround   bool
	vx, vy, vz float64
	ack        uint32

	// Recorded for the social rows, consumed by their helpers.
	Responses   []Reply
	PartyStates []*clientv1.PartyState
	Invites     []*clientv1.PartyInvited
	ChatLines   []*clientv1.ChatMessage
	Exp         []*clientv1.ExpGained
	Dialogs     []*clientv1.NpcDialog
	Quests      []*clientv1.QuestChanged

	// Recorded for the field boss rows.
	FieldBosses []Ticked[*clientv1.FieldBoss]
	BossesGone  []Ticked[*clientv1.FieldBossGone]
}

// Ticked is a message with the tick it came on, in its map's ticks.
type Ticked[T any] struct {
	Tick uint64
	Msg  T
}

// NewLooter follows p from the Snapshot it joined to.
func NewLooter(p *Player, snap *clientv1.Snapshot) *Looter {
	l := &Looter{Player: p}
	l.enter(snap)
	return l
}

// enter takes a map's Snapshot: its mobs and the player's own pouches, whose IDs and ticks are the map's own.
func (l *Looter) enter(snap *clientv1.Snapshot) {
	l.Mobs, l.Pouches, l.Seen, l.Gone, l.FellAt = map[uint32]*clientv1.MobView{}, map[uint32]*clientv1.PouchView{}, map[uint32]*clientv1.PouchView{}, map[uint32]bool{}, map[uint32]uint64{}
	for _, mv := range snap.Mobs {
		l.Mobs[mv.GetId()] = mv
	}
	for _, p := range snap.Pouches {
		l.Pouches[p.GetId()], l.Seen[p.GetId()], l.FellAt[p.GetId()] = p, p, l.tick
	}
	for _, v := range snap.Players {
		l.see(v)
	}
}

// see takes the player's own view; others' are left alone.
func (l *Looter) see(v *clientv1.PlayerView) {
	if v.GetId() == l.ID {
		l.X, l.Y, l.Z, l.HP = v.X, v.Y, v.Z, v.GetHp()
		l.onGround, l.vx, l.vy, l.vz = v.GetOnGround(), v.Vx, v.Vy, v.Vz
	}
}

// Observe takes in one message: the frames' mobs and pouches, and every inventory, reply, party, chat, exp, loot, dialog and quest message, kept in order.
func (l *Looter) Observe(m wsconn.Message) {
	switch d := m.Data.(type) {
	case *clientv1.Delta:
		l.ack = d.GetAckSeq()
		for _, v := range d.Players {
			l.see(v)
		}
		for _, mv := range d.Mobs {
			l.Mobs[mv.GetId()] = mv
		}
		for _, id := range d.RemovedMobs {
			delete(l.Mobs, id)
		}
		for _, p := range d.Pouches {
			if _, ok := l.Seen[p.GetId()]; !ok {
				l.FellAt[p.GetId()] = m.Tick
			}
			l.Pouches[p.GetId()], l.Seen[p.GetId()] = p, p
		}
		for _, id := range d.PouchesGone {
			l.Gone[id] = true
			delete(l.Pouches, id)
		}
	case *clientv1.Snapshot:
		l.enter(d)
	case *clientv1.InventoryChanged:
		l.Inventory++
	case *clientv1.Response:
		if !d.GetOk() {
			l.Failures = append(l.Failures, d)
		}
		if m.ReqID != "" && m.ReqID == l.taking {
			l.taking = ""
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
		l.FieldBosses = append(l.FieldBosses, Ticked[*clientv1.FieldBoss]{m.Tick, d})
	case *clientv1.FieldBossGone:
		l.BossesGone = append(l.BossesGone, Ticked[*clientv1.FieldBossGone]{m.Tick, d})
	}
}

// Acked is the newest input the world says it applied, as of the newest frame.
func (l *Looter) Acked() uint32 { return l.ack }

// Reply is a Response with the request id it answers.
type Reply struct {
	Req string
	R   *clientv1.Response
}

// Await reads until pred holds for a message; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) Await(d time.Duration, pred func(wsconn.Message) bool) bool {
	return l.During(d, pred, nil, nil)
}

// Expect is Player's, observing every message read on the way, as a client takes each frame: the world sends a tick's frame before its events, so a Respawned comes after the frame that placed the player.
func (l *Looter) Expect(msgType string) wsconn.Message {
	l.t.Helper()
	var got wsconn.Message
	if !l.Await(15*time.Second, func(m wsconn.Message) bool { got = m; return m.Type == msgType }) {
		l.t.Fatalf("no %s within 15 s", msgType)
	}
	return got
}

// Until reads until pick finds (and consumes) what it wants, asking after every message; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) Until(d time.Duration, pick func() bool) bool {
	return l.During(d, func(wsconn.Message) bool { return pick() }, pick, nil)
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

// Holds reports whether the pouch holds an item of the template ("" for any).
func Holds(p *clientv1.PouchView, template string) bool {
	for _, it := range p.GetItems() {
		if template == "" || it.GetTemplate() == template {
			return true
		}
	}
	return false
}

// nearestPouch is the closest of the looter's pouches holding the template ("" for any); nil with none.
func (l *Looter) nearestPouch(template string) *clientv1.PouchView {
	var best *clientv1.PouchView
	bestDist := math.Inf(1)
	for _, p := range l.Pouches {
		if !Holds(p, template) {
			continue
		}
		if dist := math.Hypot(p.GetX()-l.X, p.GetZ()-l.Z); dist < bestDist {
			best, bestDist = p, dist
		}
	}
	return best
}

// Hunt swings every swingEvery until a pouch of its own holding the template appears; it gives up once d of the world's ticks pass.
func (l *Looter) Hunt(d time.Duration, template string) (*clientv1.PouchView, bool) {
	var found *clientv1.PouchView
	ok := l.During(d, nil, func() bool { found = l.nearestPouch(template); return found != nil }, l.swings())
	return found, ok
}

// HuntPouches swings every swingEvery until n pouches of its own have been seen on the map; it gives up once d of the world's ticks pass.
func (l *Looter) HuntPouches(d time.Duration, n int) bool {
	return l.During(d, nil, func() bool { return len(l.Seen) >= n }, l.swings())
}

// All are the picks of every item in the pouch.
func All(p *clientv1.PouchView) []*clientv1.LootPick {
	var out []*clientv1.LootPick
	for _, it := range p.GetItems() {
		out = append(out, &clientv1.LootPick{Pouch: p.GetId(), Item: it.GetItem()})
	}
	return out
}

// Take sends a TakeLoot of the picks and waits for its Response, which answers each.
func (l *Looter) Take(t *testing.T, picks ...*clientv1.LootPick) *clientv1.Response {
	t.Helper()
	l.takes++
	return l.Command(t, fmt.Sprintf("take-%d", l.takes), &clientv1.TakeLoot{Picks: picks})
}

// Codes are a take's answers' codes, in its order: 0 for taken.
func Codes(r *clientv1.Response) []int32 {
	var out []int32
	for _, a := range r.GetPicks() {
		out = append(out, a.GetCode())
	}
	return out
}

// GoTake walks to the pouch and takes all it holds.
func (l *Looter) GoTake(t *testing.T, p *clientv1.PouchView) *clientv1.Response {
	t.Helper()
	if !l.WalkTo(gamedata.Point{X: p.GetX(), Z: p.GetZ()}, 20*time.Second) {
		t.Fatalf("%s could not reach pouch %d at (%.2f, %.2f) from (%.2f, %.2f)", l.ID, p.GetId(), p.GetX(), p.GetZ(), l.X, l.Z)
	}
	return l.Take(t, All(p)...)
}

// Farm hunts for d of the world's ticks: it swings every swingEvery, and walks to each pouch of its own until it's within the game's take reach and takes all it holds, one take at a time without waiting on it; it returns the InventoryChanged seen.
func (l *Looter) Farm(d time.Duration) int {
	takeFrom := 1.0 // metres; the game's reach less the slack it gives a client's lead, so a pouch a slime's body stands next to is still taken
	if data, err := l.stack.gameData(); err == nil {
		takeFrom = data.Loot().Reach - 0.5
	}
	nextAttack := l.elapsed
	walking := false
	var w Walk
	var toward uint32 // the pouch w walks to
	l.During(d, nil, nil, func() error {
		switch p := l.nearestPouch(""); {
		case p != nil && l.taking == "" && math.Hypot(p.GetX()-l.X, p.GetZ()-l.Z) <= takeFrom:
			if walking {
				_ = l.Input(&clientv1.Input{})
				walking = false
			}
			if time.Now().Before(l.nextTake) { // the server's limit on takes runs on the wall clock
				return nil
			}
			l.nextTake = time.Now().Add(500 * time.Millisecond)
			l.takes++
			l.taking = fmt.Sprintf("farm-%d", l.takes)
			return l.trySend(wsconn.Message{ReqID: l.taking, Data: &clientv1.TakeLoot{Picks: All(p)}})
		case p != nil && l.taking == "":
			if !walking || toward != p.GetId() {
				w, toward = Walk{}, p.GetId()
			}
			walking = true
			return l.Step(&w, gamedata.Point{X: p.GetX(), Z: p.GetZ()})
		case l.elapsed >= nextAttack:
			if walking {
				_ = l.Input(&clientv1.Input{})
				walking = false
			}
			nextAttack = l.elapsed + l.ticksIn(swingEvery)
			return l.Swing()
		}
		return nil
	})
	if walking {
		_ = l.Input(&clientv1.Input{})
	}
	return l.Inventory
}

// Clear waits until the looter's kills have nothing left to leave, then walks to and takes every pouch of its own, until none is left and the last take's pushes are read; each part gives up once d of the world's ticks pass.
func (l *Looter) Clear(d time.Duration) bool {
	marker := l.NextSeq() // once the world applies it, every attack sent before it is queued or done
	if err := l.trySend(wsconn.Message{Seq: marker, Data: &clientv1.Input{}}); err != nil {
		return false
	}
	var by uint64 // the tick by which a queued attack has struck
	if !l.Until(d, func() bool {
		if by == 0 && l.ack >= marker {
			by = l.tick + 2*l.attackTicks // one length to start behind the action before it, one more to strike
		}
		return by != 0 && l.tick > by
	}) {
		return false
	}
	for game := l.Game(d); game.Left(); {
		p := l.nearestPouch("")
		if p == nil && l.taking == "" {
			return l.Until(Stalled, func() bool { return l.tick > by+2 }) // a take's InventoryChanged and Loot follow its frame
		}
		if p == nil {
			if !l.nextFrame() {
				return false
			}
			continue
		}
		if !l.WalkTo(gamedata.Point{X: p.GetX(), Z: p.GetZ()}, game.Remaining()) {
			return false
		}
		l.takes++
		req := fmt.Sprintf("clear-%d", l.takes)
		if l.trySend(wsconn.Message{ReqID: req, Data: &clientv1.TakeLoot{Picks: All(p)}}) != nil {
			return false
		}
		if !l.Until(game.Remaining(), func() bool { _, gone := l.Pouches[p.GetId()]; return !gone || l.answered(req) }) {
			return false
		}
		by = l.tick
	}
	return false
}

// answered reports whether a Response to req came.
func (l *Looter) answered(req string) bool {
	for _, r := range l.Responses {
		if r.Req == req {
			return true
		}
	}
	return false
}

// LootOf is what this looter heard became of a pouch's items, of kind.
func (l *Looter) LootOf(pouch uint32, kind clientv1.LootKind) []*clientv1.Loot {
	var out []*clientv1.Loot
	for _, lt := range l.Loots {
		if lt.GetPouchId() == pouch && lt.GetKind() == kind {
			out = append(out, lt)
		}
	}
	return out
}

// Templates are the templates a Loot names, in order.
func Templates(lt *clientv1.Loot) []string {
	var out []string
	for _, it := range lt.GetItems() {
		out = append(out, it.GetTemplate())
	}
	return out
}

// DataEdit replaces Old with New in a game data file, or writes the whole file when Old is empty.
type DataEdit struct{ File, Old, New string }

// RealDataDir is the game data the server ships, with the real Willowmere: only a smoke test runs a stack on it, as generating the region is slow.
func RealDataDir() string { return gamedatatest.Dir() }

// GameData is a copy of the game data with the edits made, over the stand-ins of Willowmere, Greyfang Cave and the Sunken Shrine; a file that no longer holds what an edit replaces fails the test.
func GameData(t *testing.T, edits ...DataEdit) string {
	t.Helper()
	dir := gamedatatest.ShippedDir(t)
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

// LootRules rewrites loot.yaml: a kill's pouch chance, normal and elite alike, and a pouch's life; reach and a box's life stay.
func LootRules(chance float64, pouchLife string) DataEdit {
	return DataEdit{File: "loot.yaml", New: fmt.Sprintf("pouch: {normal: %v, elite: %v}\nreach: 2\npouch_life: %s\nbox_life: 2m\npouches_max: 30\n", chance, chance, pouchLife)}
}

// QuickSlimes has Sunmeadow's slimes in their camp die to one hit and come back a second later, so a hunt takes a shot or two.
func QuickSlimes(t testing.TB) []DataEdit {
	camp := Spots(t).Slimes
	at := fmt.Sprintf("at: [%v, %v], radius: %v, mob: green_slime, max: %d, respawn: ", camp.At.X, camp.At.Z, camp.Radius, camp.Max)
	return []DataEdit{
		{"mobs.yaml", "    name: Green Slime\n    level: 1\n    hp: 34\n", "    name: Green Slime\n    level: 1\n    hp: 1\n"},
		{"regions/willowmere.yaml", at + "24s}", at + "1s}"}, // the slimes' camp
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

// Swapper gives the character a sword, then returns a loop that wears it and the starter club in turn through the item module until stop closes and a second past it, counting the swaps that answered.
func (s *Stack) Swapper(t *testing.T, id string) func(stop <-chan struct{}) int {
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
	return func(stop <-chan struct{}) int {
		n := 0
		end := time.Now().Add(time.Hour)
		for k := 0; time.Now().Before(end); k++ {
			select {
			case <-stop:
				end, stop = time.Now().Add(time.Second), nil
			default:
			}
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
