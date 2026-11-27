package instance_test

import (
	"context"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
)

// plainSize is the interest tests' map: flat ground ten 32 m cells each way.
const plainSize = 320.0

// plainGround is the plain's geometry and nav grid, built once: every plain is the same flat ground.
var plainGround struct {
	once sync.Once
	m    *gamedata.Map
}

func plain(t *testing.T, spawns []gamedata.Spawn, boss *gamedata.BossSpot) *gamedata.Map {
	t.Helper()
	plainGround.once.Do(func() { plainGround.m = worldtest.Gridded(t, &gamedata.Map{Geometry: worldtest.Flat(plainSize)}) })
	return &gamedata.Map{
		ID: "plain", Kind: gamedata.Hunting, Town: "willowmere", Geometry: plainGround.m.Geometry, Grid: plainGround.m.Grid, Spawns: spawns, Boss: boss,
		PlayerSpawn: gamedata.Point{X: plainSize / 2, Z: plainSize / 2},
	}
}

// slimeAt is a camp of one slime standing at (x, z).
func slimeAt(x, z float64) gamedata.Spawn {
	return gamedata.Spawn{Mob: "green_slime", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: x, Z: z}}
}

// sight is what a client holds of the map from a player's snapshot and frames: each body's latest view, less what left or went out of sight, and the boss for its bar.
type sight struct {
	players map[kernel.CharacterID]model.PlayerView
	mobs    map[uint64]model.MobView
	boss    *model.MobView
}

func sightOf(t *testing.T, s *worldtest.Sink) sight {
	t.Helper()
	var v sight
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			v = sight{players: map[kernel.CharacterID]model.PlayerView{}, mobs: map[uint64]model.MobView{}, boss: ev.Boss}
			for _, p := range ev.Players {
				v.players[p.ID] = p
			}
			for _, m := range ev.Mobs {
				v.mobs[m.ID] = m
			}
		case worldtest.Delta:
			for _, p := range ev.Players {
				v.players[p.ID] = p
			}
			for _, m := range ev.Mobs {
				v.mobs[m.ID] = m
			}
			for _, id := range ev.Removed {
				delete(v.players, id)
			}
			for _, id := range ev.RemovedMobs {
				delete(v.mobs, id)
			}
			for _, id := range ev.Unseen.Players {
				if _, held := v.players[id]; !held {
					t.Fatalf("tick %d: player %v out of sight, never seen", ev.Tick, id)
				}
				delete(v.players, id)
			}
			for _, id := range ev.Unseen.Mobs {
				if _, held := v.mobs[id]; !held {
					t.Fatalf("tick %d: mob %d out of sight, never seen", ev.Tick, id)
				}
				delete(v.mobs, id)
			}
			if ev.Boss != nil {
				v.boss = ev.Boss
			}
		}
	}
	return v
}

// A player joining at any corner of a cell, its worst spots, is sent every mob in the cells round theirs, 3 columns each side, 1 row behind and 3 ahead, and none past them: every mob within 96 m to each side, 32 m behind and 96 m ahead.
func TestAJoinIsSentTheCellsRoundItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		in := data.Interest()
		r := rand.New(rand.NewPCG(7, 11))
		var spawns []gamedata.Spawn
		for range 400 {
			spawns = append(spawns, slimeAt(0.25+0.5*float64(r.IntN(int(plainSize*2))), 0.25+0.5*float64(r.IntN(int(plainSize*2))))) // never on a cell's edge
		}
		inst, stop := worldtest.RunMap(t, data, plain(t, spawns, nil), nil, worldtest.Options{})
		defer stop()
		cellOf := func(x, z float64) (int, int) { return int(math.Floor(x / in.Cell)), int(math.Floor(z / in.Cell)) }
		side, behind, ahead := float64(in.Side)*in.Cell, float64(in.Behind)*in.Cell, float64(in.Ahead)*in.Cell
		for _, at := range [][2]float64{{160.05, 160.05}, {191.95, 160.05}, {160.05, 191.95}, {191.95, 191.95}} {
			ayla := worldtest.NewPlayer("Ayla", at[0], at[1])
			snap := eventsOf[model.Snapshot](join(t, inst, ayla))[0]
			if !slices.ContainsFunc(snap.Players, func(v model.PlayerView) bool { return v.ID == ayla.ID }) {
				t.Fatal("a joiner's snapshot doesn't hold them")
			}
			sent := map[uint64]bool{}
			for _, m := range snap.Mobs {
				sent[m.ID] = true
			}
			col, row := cellOf(at[0], at[1])
			for k, sp := range spawns {
				id := uint64(k + 1) // mobs are numbered from 1 in spawn order
				c, r := cellOf(sp.At.X, sp.At.Z)
				inBlock := c >= col-in.Side && c <= col+in.Side && r >= row-in.Behind && r <= row+in.Ahead
				dx, dz := sp.At.X-at[0], sp.At.Z-at[1]
				near := math.Abs(dx) <= side && dz >= -behind && dz <= ahead
				if sent[id] != inBlock || near && !sent[id] {
					t.Fatalf("a player at (%v, %v): the mob at (%v, %v) sent %v, want %v as its cell is in the block or not, and sent within %v m to each side, %v m behind and %v m ahead", at[0], at[1], sp.At.X, sp.At.Z, sent[id], inBlock, side, behind, ahead)
				}
			}
		}
	})
}

// A player walking north leaves the cells round another's: each stops seeing the other, the one ahead first, as the cells reach 1 row behind and 3 ahead, with no removal; walking back, each sees the other again whole.
func TestPlayersGoOutOfSightAndComeBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		ayla, robin := worldtest.NewPlayer("Ayla", 160, 10), worldtest.NewPlayer("Robin", 162, 10) // row 0
		sa, sr := join(t, inst, ayla), join(t, inst, robin)
		if _, ok := sightOf(t, sa).players[robin.ID]; !ok {
			t.Fatal("Ayla doesn't see Robin beside her")
		}
		row := func(id kernel.CharacterID, s *worldtest.Sink) int {
			p, _ := s.Own(id)
			return int(p.State.Z / data.Interest().Cell)
		}
		seq := uint32(0)
		for row(robin.ID, sr) < 2 { // Ayla is 2 rows behind Robin, past the 1 his cells reach
			seq = worldtest.Walk(inst, robin.ID, seq, sim.Input{Up: true}, 1)
		}
		if _, sees := sightOf(t, sr).players[ayla.ID]; sees {
			t.Fatal("Robin still sees Ayla 2 rows behind him")
		}
		if _, sees := sightOf(t, sa).players[robin.ID]; !sees {
			t.Fatal("Ayla lost Robin 2 rows ahead of her")
		}
		for row(robin.ID, sr) < 4 { // he's past the 3 rows hers reach
			seq = worldtest.Walk(inst, robin.ID, seq, sim.Input{Up: true}, 1)
		}
		if _, sees := sightOf(t, sa).players[robin.ID]; sees {
			t.Fatal("Ayla still sees Robin 4 rows ahead of her")
		}
		for _, s := range []*worldtest.Sink{sa, sr} {
			for _, d := range s.Deltas() {
				if len(d.Removed) > 0 {
					t.Fatalf("removed %v: out of sight is not off the map", d.Removed)
				}
			}
		}
		for row(robin.ID, sr) > 0 {
			seq = worldtest.Walk(inst, robin.ID, seq, sim.Input{Down: true}, 1)
		}
		tick(1)
		back, sees := sightOf(t, sr).players[ayla.ID]
		her, _ := sa.Own(ayla.ID)
		if !sees || back.State != her.State || back.Name != "Ayla" {
			t.Fatalf("Robin back sees Ayla %v as %+v, want her whole as she is, %+v", sees, back, her)
		}
		if _, sees := sightOf(t, sa).players[robin.ID]; !sees {
			t.Fatal("Ayla doesn't see Robin back beside her")
		}
	})
}

// A shot across a cell line goes to the watchers of either end: one who sees only the shooter's cell hears the swing and the hit, one who sees only the target's the hit, and one who sees neither nothing.
func TestAShotsEventsGoToTheWatchersOfEitherEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cell := data.Interest().Cell
		slimeZ := 4*cell + 0.5 // just inside row 4
		inst, stop := worldtest.RunMap(t, data, plain(t, []gamedata.Spawn{slimeAt(160, slimeZ)}, nil), nil, worldtest.Options{})
		defer stop()
		shooter := bowmanAt(slimeZ - 11) // row 3, past the slime's sight and within the bow's range
		shooter.X = 160
		shooter.Worn.Items[0].Stats.PhysicalAttack = 60                       // a shot that kills
		swingSide := join(t, inst, worldtest.NewPlayer("Ayla", 100, 10))      // row 0: rows 0 to 3
		hitSide := join(t, inst, worldtest.NewPlayer("Bruno", 100, 5*cell+5)) // row 5: rows 4 to 8
		neither := join(t, inst, worldtest.NewPlayer("Cara", 100, 9*cell+5))  // row 9: rows 8 and 9
		s := join(t, inst, shooter)
		_ = inst.Attack(shooter.ID, 0, model.EntityRef{})
		tick(10)
		by := model.EntityRef{Player: shooter.ID}
		hits := func(s *worldtest.Sink) int {
			n := 0
			for _, d := range eventsOf[model.Damage](s) {
				if d.Source == by && d.Target == (model.EntityRef{Mob: 1}) {
					n++
				}
			}
			return n
		}
		if hits(s) != 1 || len(mobsIn(s)) != 0 {
			t.Fatalf("the shooter heard %d hits on the slime and holds mobs %v, want its one shot's, which kills", hits(s), mobsIn(s))
		}
		if _, held := sightOf(t, hitSide).mobs[1]; held {
			t.Fatal("the target's side holds the slime the shot killed")
		}
		for _, c := range []struct {
			name         string
			s            *worldtest.Sink
			swings, hits int
		}{{"the shooter's side", swingSide, 1, 1}, {"the target's side", hitSide, 0, 1}, {"neither side", neither, 0, 0}} {
			if swung := len(eventsOf[model.AttackUsed](c.s)); swung != c.swings || hits(c.s) != c.hits {
				t.Errorf("%s: heard %d swings and %d hits, want %d and %d", c.name, swung, hits(c.s), c.swings, c.hits)
			}
		}
	})
}

// Players and mobs standing still at the block's every edge and corner, and just past them, are sent once, in the snapshot, those in it and none past; nothing of them after.
func TestAStillPlayerHearsNothingMoreOfStillMobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		in := data.Interest()
		mid := func(k int) float64 { return (float64(k) + 0.5) * in.Cell }
		var spawns []gamedata.Spawn
		inBlock := map[uint64]bool{}
		for _, col := range []int{5 - in.Side - 1, 5 - in.Side, 5, 5 + in.Side, 5 + in.Side + 1} {
			for _, row := range []int{5 - in.Behind - 1, 5 - in.Behind, 5, 5 + in.Ahead, 5 + in.Ahead + 1} {
				if col == 5 && row == 5 {
					continue
				}
				spawns = append(spawns, slimeAt(mid(col), mid(row)))
				inBlock[uint64(len(spawns))] = col >= 5-in.Side && col <= 5+in.Side && row >= 5-in.Behind && row <= 5+in.Ahead
			}
		}
		inst, stop := worldtest.RunMap(t, data, plain(t, spawns, nil), nil, worldtest.Options{})
		defer stop()
		tick(2) // the spawns, new to the first frame, go out on a map nobody is on yet
		s := join(t, inst, worldtest.NewPlayer("Ayla", mid(5), mid(5)))
		tick(40)
		held := sightOf(t, s).mobs
		for id, want := range inBlock {
			if _, ok := held[id]; ok != want {
				t.Errorf("mob %d at (%v, %v) held %v, want %v", id, spawns[id-1].At.X, spawns[id-1].At.Z, ok, want)
			}
		}
		for _, d := range s.Deltas() {
			if len(d.Mobs) > 0 {
				t.Fatalf("tick %d sent still mobs %+v again", d.Tick, d.Mobs)
			}
		}
	})
}

// A player leaving the map on the tick another's cells move off theirs goes out of sight for the other, whose frames no longer carry that cell.
func TestALeaverOnTheTickTheBlockMovesOffGoesOutOfSight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		line := 2 * data.Interest().Cell // Ayla crossing it into row 2 leaves row 0 behind her block
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		ayla, robin := worldtest.NewPlayer("Ayla", 160, line-6), worldtest.NewPlayer("Robin", 160, 10)
		s := join(t, inst, ayla)
		join(t, inst, robin)
		z := func() float64 { p, _ := s.Own(ayla.ID); return p.State.Z }
		seq, last := uint32(0), z()
		for {
			seq = worldtest.Walk(inst, ayla.ID, seq, sim.Input{Up: true}, 1)
			now := z()
			if now+(now-last) >= line {
				break
			}
			last = now
		}
		if err := inst.Leave(robin.ID, "", nil); err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, ayla.ID, seq, sim.Input{Up: true}, 1)
		if z() < line {
			t.Fatalf("Ayla at z %v hasn't crossed %v on the leave's tick", z(), line)
		}
		if _, held := sightOf(t, s).players[robin.ID]; held || !slices.Contains(s.Last().Unseen.Players, robin.ID) {
			t.Fatalf("Ayla holds Robin %v, out of sight %+v; want him out of her sight on that tick", held, s.Last().Unseen)
		}
	})
}

// A mob that comes back while nobody is on the map is in the next joiner's snapshot: frames sent nobody its coming, so the map's bodies are filed afresh.
func TestAMobBackOnAnEmptyMapIsInTheNextSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		camp := slimeAt(160, 160)
		camp.Respawn = time.Second
		inst, stop := worldtest.RunMap(t, data, plain(t, []gamedata.Spawn{camp}, nil), nil, worldtest.Options{})
		defer stop()
		p := warriorAt(158.5) // in a swing's reach of the camp's spot, facing it
		p.X = 160
		p.Worn.Items[0].Stats.PhysicalAttack = 200 // a swing kills
		s := join(t, inst, p)
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		for k := 0; !slices.ContainsFunc(s.Deltas(), func(d worldtest.Delta) bool { return slices.Contains(d.RemovedMobs, 1) }); k++ {
			if k == 100 {
				t.Fatal("the camp's mob never died")
			}
			tick(1)
		}
		if err := inst.Leave(p.ID, "", nil); err != nil {
			t.Fatal(err)
		}
		tick(2 * int(time.Second/worldtest.TickInterval)) // the camp's next comes
		snap := eventsOf[model.Snapshot](join(t, inst, worldtest.NewPlayer("Robin", 160, 150)))[0]
		if !slices.ContainsFunc(snap.Mobs, func(m model.MobView) bool { return m.ID == 2 }) {
			t.Fatalf("snapshot mobs %+v; want the camp's next, which came while the map was empty", snap.Mobs)
		}
	})
}

// A player coming to a map left empty hears nothing of those who left it before.
func TestAPlayerOnAMapLeftEmptyHearsNothingOfThoseWhoLeft(t *testing.T) {
	bothPaths(t, func(t *testing.T, side float64) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arenaOf(t, time.Hour, side), nil, worldtest.Options{})
		defer stop()
		ayla := worldtest.NewPlayer("Ayla", arenaX, spawnZ)
		join(t, inst, ayla)
		if err := inst.Leave(ayla.ID, "", nil); err != nil {
			t.Fatal(err)
		}
		tick(5)
		s := join(t, inst, worldtest.NewPlayer("Robin", arenaX, spawnZ))
		tick(5)
		for _, d := range s.Deltas() {
			if slices.Contains(d.Removed, ayla.ID) {
				t.Fatalf("Robin was told at tick %d that Ayla, who left before he came, left", d.Tick)
			}
		}
	})
}

// A player whose join failed never reached a frame, so nobody is told they left.
func TestAJoinThatFailedIsNeverToldGone(t *testing.T) {
	bothPaths(t, func(t *testing.T, side float64) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arenaOf(t, time.Hour, side), nil, worldtest.Options{})
		defer stop()
		s := join(t, inst, worldtest.NewPlayer("Bruno", arenaX, spawnZ))
		ayla := worldtest.NewPlayer("Ayla", arenaX, spawnZ)
		if err := inst.Join(context.Background(), ayla, worldtest.Refusing()); err == nil {
			t.Fatal("a join through a closed sink went through")
		}
		tick(5)
		for _, d := range s.Deltas() {
			if slices.Contains(d.Removed, ayla.ID) {
				t.Fatalf("Bruno was told at tick %d that Ayla, whose join failed, left", d.Tick)
			}
		}
	})
}

// bothPaths runs f in a bubble on ground 40 m square, sent whole as today's maps are, and on ground 320 m square, cut by cells.
func bothPaths(t *testing.T, f func(t *testing.T, side float64)) {
	for _, side := range []float64{40, plainSize} {
		t.Run(fmt.Sprintf("%v m", side), func(t *testing.T) { synctest.Test(t, func(t *testing.T) { f(t, side) }) })
	}
}

// A mob killed on the tick it spawns is sent as it was, with its removal, where it stood, so its killer's client can draw the hit; no client keeps it.
func TestAMobKilledAsItSpawnsIsSentWithItsRemoval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		camp := slimeAt(160, 160)
		camp.Respawn = time.Second
		inst, stop := worldtest.RunMap(t, data, plain(t, []gamedata.Spawn{camp}, nil), nil, worldtest.Options{})
		defer stop()
		p := warriorAt(158.5) // in a swing's reach of the camp's spot, facing it
		p.X = 160
		p.Worn.Items[0].Stats.PhysicalAttack = 200 // a swing kills
		s := join(t, inst, p)
		killedAt := func(id uint64) uint64 {
			for range 100 {
				for _, d := range s.Deltas() {
					if slices.Contains(d.RemovedMobs, id) {
						return d.Tick
					}
				}
				tick(1)
			}
			t.Fatalf("mob %d never died", id)
			return 0
		}
		cameAt := func(id uint64) uint64 {
			for range 100 {
				for _, d := range s.Deltas() {
					if slices.ContainsFunc(d.Mobs, func(m model.MobView) bool { return m.ID == id }) {
						return d.Tick
					}
				}
				tick(1)
			}
			t.Fatalf("mob %d never came", id)
			return 0
		}
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		first := killedAt(1)
		swing := eventsOf[model.AttackUsed](s)[0].Tick
		lands := first - swing    // ticks from a swing's start to its hit
		back := cameAt(2) - first // ticks from a death to the camp's next mob
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		second := killedAt(2)
		for s.Last().Tick < second+back-lands-1 {
			tick(1)
		}
		_ = inst.Attack(p.ID, 0, model.EntityRef{}) // its swing starts next tick and lands as mob 3 spawns
		for s.Last().Tick < second+back {
			tick(1)
		}
		d := s.Last()
		if !slices.ContainsFunc(d.Mobs, func(m model.MobView) bool { return m.ID == 3 && m.HP <= 0 }) || !slices.Contains(d.RemovedMobs, 3) {
			t.Fatalf("tick %d sent mobs %+v, removed %v; want mob 3 as it spawned, dead, and its removal", d.Tick, d.Mobs, d.RemovedMobs)
		}
		tick(1)
		if _, held := sightOf(t, s).mobs[3]; held {
			t.Fatal("the client holds mob 3, killed as it spawned")
		}
	})
}

// A map a row taller than the block holds from its south edge is cut by cells: a player on its north row doesn't see its south row, two behind, which sees them.
func TestAMapARowPastTheBlockIsCutByCells(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cell := data.Interest().Cell
		side := float64(data.Interest().Behind+2) * cell // rows 0 to 2
		m := worldtest.Gridded(t, &gamedata.Map{ID: "tall", Kind: gamedata.Hunting, Town: "willowmere", Geometry: worldtest.Flat(side), PlayerSpawn: gamedata.Point{X: 10, Z: 10}})
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		south, north := worldtest.NewPlayer("Ayla", 10, 10), worldtest.NewPlayer("Robin", 10, side-10)
		ss, sn := join(t, inst, south), join(t, inst, north)
		tick(2)
		if _, sees := sightOf(t, sn).players[south.ID]; sees {
			t.Fatal("the north row sees the south row two behind it")
		}
		if _, sees := sightOf(t, ss).players[north.ID]; !sees {
			t.Fatal("the south row doesn't see the north row two ahead")
		}
	})
}

// A player whose rejoin, over a stale presence, fails to take its snapshot is off the map for good: another walking over their spot never sees them.
func TestAFailedRejoinLeavesNoBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cell := data.Interest().Cell
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		join(t, inst, worldtest.NewPlayer("Bruno", 300, 300)) // keeps the map watched, so nothing is filed afresh
		ayla := worldtest.NewPlayer("Ayla", 20, 20)
		join(t, inst, ayla)
		if err := inst.Join(context.Background(), ayla, worldtest.Refusing()); err == nil {
			t.Fatal("a rejoin through a closed sink went through")
		}
		robin := worldtest.NewPlayer("Robin", 20, 5*cell+16) // row 5: rows 4 to 8, past her row 0
		s := join(t, inst, robin)
		p, _ := s.Own(robin.ID)
		for seq := uint32(0); p.State.Z >= cell; p, _ = s.Own(robin.ID) { // to row 0
			seq = worldtest.Walk(inst, robin.ID, seq, sim.Input{Down: true}, 1)
		}
		if _, held := sightOf(t, s).players[ayla.ID]; held {
			t.Fatal("Robin sees Ayla, whose rejoin failed")
		}
	})
}

// A player far from the field boss is sent it for its bar, on the snapshot and each frame it changes, but never among the mobs drawn; one near it gets both.
func TestAFarPlayerIsSentTheFieldBossForItsBarOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := plain(t, nil, &gamedata.BossSpot{Mob: "king_slime", At: gamedata.Point{X: 300, Z: 300}, Radius: 6})
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		far, near := join(t, inst, worldtest.NewPlayer("Ayla", 20, 20)), join(t, inst, worldtest.NewPlayer("Robin", 290, 290))
		if snap := eventsOf[model.Snapshot](far)[0]; snap.Boss == nil || len(snap.Mobs) != 0 {
			t.Fatalf("the far player's snapshot: boss %+v, mobs %+v; want the boss for its bar alone", snap.Boss, snap.Mobs)
		}
		tick(200) // it wanders its circle
		for _, c := range []struct {
			name  string
			s     *worldtest.Sink
			drawn bool
		}{{"far", far, false}, {"near", near, true}} {
			v := sightOf(t, c.s)
			_, drawn := v.mobs[v.boss.ID]
			moved := 0
			for _, d := range c.s.Deltas() {
				if d.Boss != nil {
					moved++
				}
			}
			if v.boss == nil || drawn != c.drawn || moved == 0 {
				t.Fatalf("the %s player: boss %+v, drawn %v, %d frames with it; want it, drawn %v, in frames as it wanders", c.name, v.boss, drawn, moved, c.drawn)
			}
		}
	})
}

// Players wander a crowded map, fighting what they meet: whatever each one's client holds at the end, from its snapshot, frames and what went out of sight, is what a player who joins on its spot holds a frame on.
func TestEveryClientHoldsWhatAJoinOnItsSpotWouldSee(t *testing.T) {
	for seed := range uint64(3) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				r := rand.New(rand.NewPCG(seed, 3))
				var spawns []gamedata.Spawn
				kinds := []string{"green_slime", "wild_boar", "cave_bat", "orange_mushroom"}
				for range 60 {
					spawns = append(spawns, gamedata.Spawn{Mob: kinds[r.IntN(len(kinds))], Max: 2, Respawn: 5 * time.Second, Radius: 8, At: gamedata.Point{X: 10 + r.Float64()*300, Z: 10 + r.Float64()*300}})
				}
				inst, stop := worldtest.RunMap(t, data, plain(t, spawns, nil), nil, worldtest.Options{})
				defer stop()
				type walker struct {
					p    model.Player
					s    *worldtest.Sink
					seq  uint32
					in   sim.Input
					left int
				}
				var walkers []*walker
				for k := range 6 {
					p := worldtest.NewPlayer(fmt.Sprintf("P%d", k), 20+r.Float64()*280, 20+r.Float64()*280)
					p.Level, p.HP = 60, 5000
					walkers = append(walkers, &walker{p: p, s: join(t, inst, p)})
				}
				for range 600 { // half a minute, a few cells' walk each
					for _, w := range walkers {
						if w.left == 0 {
							w.in, w.left = sim.Input{Left: r.IntN(3) == 0, Right: r.IntN(3) == 0, Up: r.IntN(2) == 0, Down: r.IntN(3) == 0}, 20+r.IntN(80)
						}
						w.left--
						w.seq++
						_ = inst.Input(w.p.ID, w.seq, w.in, 0)
						if r.IntN(30) == 0 {
							_ = inst.Attack(w.p.ID, float32(r.Float64()*2*math.Pi), model.EntityRef{})
						}
					}
					tick(1)
				}
				tick(2)
				for _, w := range walkers {
					me, _ := w.s.Own(w.p.ID)
					observer := worldtest.NewPlayer("Observer", me.State.X, me.State.Z)
					seen := join(t, inst, observer)
					held, sent := sightOf(t, w.s), sightOf(t, seen) // both as of the same frame
					delete(held.players, observer.ID)
					delete(sent.players, observer.ID)
					compare(t, w.p.Name, held, sent)
					if err := inst.Leave(observer.ID, "", nil); err != nil {
						t.Fatal(err)
					}
					tick(1)
				}
			})
		})
	}
}

// compare fails unless the client holds what the join was sent: the same players and mobs where they stand.
func compare(t *testing.T, name string, held, sent sight) {
	t.Helper()
	ids := func(m map[uint64]model.MobView) []uint64 { return slices.Sorted(maps.Keys(m)) }
	if len(held.players) != len(sent.players) || len(held.mobs) != len(sent.mobs) {
		t.Fatalf("%s holds %d players and mobs %v; a join there is sent %d and %v", name, len(held.players), ids(held.mobs), len(sent.players), ids(sent.mobs))
	}
	for id, p := range sent.players {
		if h, ok := held.players[id]; !ok || quant.Position(h.State.X) != quant.Position(p.State.X) || quant.Position(h.State.Z) != quant.Position(p.State.Z) { // each as the wire has it: to the centimetre
			t.Fatalf("%s holds player %s as %v (%v), a join is sent %+v", name, p.Name, h.State, ok, p.State)
		}
	}
	for id, m := range sent.mobs {
		if h, ok := held.mobs[id]; !ok || quant.Position(h.X) != quant.Position(m.X) || quant.Position(h.Z) != quant.Position(m.Z) || h.HP != m.HP {
			t.Fatalf("%s holds mob %d as %+v (%v), a join is sent %+v", name, id, h, ok, m)
		}
	}
}
