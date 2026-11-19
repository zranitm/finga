package mobs_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

const tick = 50 * time.Millisecond

// A player's body, as the shipped movement.yaml has it.
const playerRadius, playerHeight = 0.375, 1.75

// open is flat ground 40 m square, with boxes on it.
func open(boxes ...sim.Box) sim.Geometry {
	return sim.Geometry{SizeX: 40, SizeZ: 40, Terrain: sim.NewTerrain(40, 2, 2, []float64{0, 0, 0, 0}, 0.7), Boxes: boxes, Cylinders: []sim.Cylinder{}}
}

// meadow is a hunting map on g with spawns.
func meadow(t testing.TB, g sim.Geometry, spawns ...gamedata.Spawn) *gamedata.Map {
	t.Helper()
	grid := nav.New(g, worldtest.LoadData(t).Movement().Step)
	return &gamedata.Map{ID: "meadow", Kind: gamedata.Hunting, Geometry: g, Spawns: spawns, Grid: grid}
}

// spawn is n of mob in a circle of radius r about (x, z), each back 5 s after its kill.
func spawn(mob string, n int, x, z, r float64) gamedata.Spawn {
	return gamedata.Spawn{Mob: mob, Max: n, Respawn: 5 * time.Second, At: gamedata.Point{X: x, Z: z}, Radius: r}
}

func newArena(t testing.TB, m *gamedata.Map, roll model.Roll) *mobs.Arena {
	t.Helper()
	return mobs.NewArena(mobs.Deps{Map: m, Data: worldtest.LoadData(t), Roll: roll}, mobs.Tuning{TickInterval: tick})
}

// slimes is two green slimes in a circle 2 m round (20, 20); at a roll of 0.5 both spawn at (20, 20 - √2), facing north, and stay.
func slimes(t *testing.T, roll float64) *mobs.Arena {
	t.Helper()
	return newArena(t, meadow(t, open(), spawn("green_slime", 2, 20, 20, 2)), worldtest.FixedRoll{V: roll})
}

// alone is one mob of type id at (20, z0) on open ground, facing north, where z0 is a hair under 20; it stays there unless led off.
func alone(t *testing.T, id string) *mobs.Arena {
	t.Helper()
	return newArena(t, meadow(t, open(), spawn(id, 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
}

// cycle rolls its values in turn, over and over.
type cycle struct {
	v []float64
	i int
}

func (c *cycle) Float64() float64 {
	x := c.v[c.i%len(c.v)]
	c.i++
	return x
}

// player is a player's body with its feet at (x, 0, z).
func player(x, z float64) mobs.Body {
	return mobs.Body{ID: kernel.NewCharacterID(), X: x, Z: z, Radius: playerRadius, Height: playerHeight}
}

// strong hits whatever it reaches, for more than a slime's HP.
var strong = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1000, Accuracy: 100}

// swing is a Warrior's basic attack from (x, z) along yaw: an arc of 2.375 m and 120°, 1.75 m high.
func swing(x, z, yaw float64) mobs.Strike {
	return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.ArcShape, Radius: 2.375, Angle: 120, Height: 1.75}, Most: 1, Power: strong, Aim: model.Aim{X: x, Z: z, Yaw: yaw}}
}

// one is the only blow of a strike, if it reached anything.
func one(t *testing.T, blows []mobs.Blow) (mobs.Blow, bool) {
	t.Helper()
	if len(blows) > 1 {
		t.Fatalf("%d blows; want one at most", len(blows))
	}
	if len(blows) == 0 {
		return mobs.Blow{}, false
	}
	return blows[0], true
}

func distance(v model.MobView, x, z float64) float64 { return math.Hypot(v.X-x, v.Z-z) }

func TestTheArenaIsFullBeforeItsFirstTick(t *testing.T) {
	views := slimes(t, 0.5).Views()
	if len(views) != 2 || views[0].ID == views[1].ID {
		t.Fatalf("views %+v; want the spawn's two slimes", views)
	}
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	for _, v := range views {
		if v.Type != "green_slime" || distance(v, 20, 20) > 2 || v.Y != 0 || v.HP != slime.HP || v.MaxHP != slime.HP || v.Yaw != 0 {
			t.Fatalf("spawned %+v; want a slime on the ground in its circle, at full HP, facing north", v)
		}
	}
}

// A slime walks to a spot in its circle, waits a rolled while, and walks to another, never leaving its circle or outpacing itself.
func TestAMobWandersItsCircleAndPausesAtEachSpot(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 1, 20, 20, 3)), &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25}})
	data := worldtest.LoadData(t)
	slime, _ := data.Mob("green_slime")
	fastest := slime.Speed * (1 + data.MobRules().Patrol.SpeedJitter) * tick.Seconds()
	last, still, pauses := a.Views()[0], 0, 0
	for n := range uint64(600) {
		a.Step(n+1, nil, nil)
		v := a.Views()[0]
		step := distance(v, last.X, last.Z)
		if step > fastest+1e-9 || distance(v, 20, 20) > 3+slime.Radius {
			t.Fatalf("tick %d: stepped %.3f m to (%.2f, %.2f); want at most %.3f m a tick, inside its circle", n+1, step, v.X, v.Z, fastest)
		}
		if step == 0 {
			still++
			continue
		}
		if still >= int(data.MobRules().Patrol.PauseMin/tick) {
			pauses++ // it set off again after a pause
		}
		last, still = v, 0
	}
	if pauses < 3 {
		t.Fatalf("set off after a pause %d times in 30 s; want it wandering from spot to spot", pauses)
	}
}

func TestAKilledMobComesBackAfterItsRespawn(t *testing.T) {
	a := slimes(t, 0.5)
	a.Step(1, nil, nil)
	_, _ = a.Changes()
	stood := a.Views()[0]
	blow, reached := one(t, a.Strike(swing(stood.X, stood.Z-1.5, 0)))
	if !reached || blow.Killed == nil || blow.Killed.Mob.ID != "green_slime" || blow.Killed.X != stood.X || blow.Killed.Y != stood.Y || blow.Killed.Z != stood.Z {
		t.Fatalf("blow %+v reached %v; want the slime killed where it stood", blow, reached)
	}
	if changed, removed := a.Changes(); len(changed) != 0 || len(removed) != 1 || removed[0] != blow.Damage.Target.Mob {
		t.Fatalf("changed %+v, removed %v; want the killed slime removed, its view told before", changed, removed)
	}
	back := 1 + uint64(5*time.Second/tick)
	for n := uint64(2); n < back; n++ {
		a.Step(n, nil, nil)
		if got := len(a.Views()); got != 1 {
			t.Fatalf("tick %d: %d slimes; want one until the respawn", n, got)
		}
	}
	a.Step(back, nil, nil)
	views := a.Views()
	if len(views) != 2 || views[1].ID == blow.Damage.Target.Mob {
		t.Fatalf("tick %d: %+v; want a new slime back", back, views)
	}

	// Killed on the tick it came back, before any frame showed it: the frame tells it as it was with its removal, so a client can draw the hit on it.
	fresh := views[1]
	again, _ := one(t, a.Strike(swing(fresh.X, fresh.Z-1.5, 0)))
	if again.Killed == nil || again.Damage.Target.Mob != fresh.ID {
		t.Fatalf("blow %+v; want the new slime killed", again)
	}
	told := func(changed []model.MobView) []model.MobView {
		return slices.DeleteFunc(changed, func(v model.MobView) bool { return v.ID != fresh.ID })
	}
	changed, removed := a.Changes()
	if got := told(changed); len(got) != 1 || got[0].X != fresh.X || got[0].Z != fresh.Z || len(removed) != 1 || removed[0] != fresh.ID {
		t.Fatalf("changed %+v, removed %v; want the new slime told where it stood, and removed", changed, removed)
	}
	if changed, _ := a.Changes(); len(told(changed)) != 0 {
		t.Fatalf("changed %+v; want it told once", changed)
	}
}

func TestAStrikeReachesOnlyWhatItsShapeCovers(t *testing.T) {
	shot := func(x, z, rng float64) mobs.Strike {
		return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.ShotShape, Range: rng, Width: 0.6}, Most: 1, Power: strong, Aim: model.Aim{X: x, Z: z}}
	}
	area := mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 2}, Most: 1, Power: strong, Aim: model.Aim{X: 21, Z: 20}}
	above := swing(20, 18, 0)
	above.Aim.Y = 3
	tests := []struct {
		name    string
		strike  mobs.Strike
		reached bool
	}{
		{"a swing at the slime", swing(20, 18, 0), true},
		{"a swing facing away", swing(20, 18, math.Pi), false},
		{"a swing out of reach", swing(20, 16, 0), false},
		{"a swing from 3 m up", above, false},
		{"a shot in range", shot(20, 10, 10), true},
		{"a shot that falls short", shot(20, 10, 9), false},
		{"a shot passing beside", shot(21, 10, 12), false},
		{"an area round the attacker", area, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blow, reached := one(t, alone(t, "green_slime").Strike(tt.strike))
			if reached != tt.reached {
				t.Fatalf("reached %v, want %v", reached, tt.reached)
			}
			if reached && (blow.Damage.Source.Player != tt.strike.By || !blow.Damage.Target.IsMob() || blow.Damage.Amount < 30) {
				t.Fatalf("damage %+v", blow.Damage)
			}
		})
	}
}

// A strike that misses still reaches its slime, which keeps its HP.
func TestAMissHurtsNothing(t *testing.T) {
	a := slimes(t, 0.995) // past the best hit chance
	v := a.Views()[0]
	blow, reached := one(t, a.Strike(swing(v.X, v.Z-1.5, 0)))
	if !reached || !blow.Damage.Miss || blow.Damage.Amount != 0 || blow.Killed != nil {
		t.Fatalf("blow %+v reached %v; want a miss", blow, reached)
	}
	for _, v := range a.Views() {
		if v.HP != v.MaxHP {
			t.Fatalf("a slime lost HP to a miss: %+v", v)
		}
	}
}

func TestAMagicStrikeMeetsAMobsMagicDefense(t *testing.T) {
	s := swing(20, 18, 0)
	s.Power = model.Attacker{Damage: gamedata.Magic, MagicAttack: 30.3, PhysicalAttack: 1000, Accuracy: 100}
	blow, reached := one(t, alone(t, "green_slime").Strike(s))
	// 30.3·100/101 = 30
	if !reached || blow.Damage.Amount != 30 || blow.Killed != nil {
		t.Fatalf("blow %+v reached %v; want 30 against magic defense 1", blow, reached)
	}
}

// A strike for several hits each mob its shape reaches, nearest first, up to its most.
func TestAStrikeHitsUpToItsMostNearestFirst(t *testing.T) {
	area := func(x float64, most int) mobs.Strike {
		return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 3}, Most: most, Power: strong, Aim: model.Aim{X: x, Z: 20}}
	}
	if blows := slimes(t, 0.5).Strike(area(20, 3)); len(blows) != 2 || blows[0].Killed == nil || blows[1].Killed == nil {
		t.Fatalf("blows %+v; want both slimes, killed", blows)
	}
	a := slimes(t, 0.5)
	if blows := a.Strike(area(20, 1)); len(blows) != 1 || len(a.Views()) != 1 {
		t.Fatalf("blows %+v, %d left; want one slime hit and one left", blows, len(a.Views()))
	}
	if blows := slimes(t, 0.5).Strike(area(30, 3)); len(blows) != 0 {
		t.Fatalf("blows %+v; want none beyond the radius", blows)
	}
}

// A strike takes its target first when its shape reaches it, then the nearest; a target it misses, an unknown one or none leaves the nearest first, as bots send none.
func TestAStrikeTakesItsTargetFirstWhenItsShapeReachesIt(t *testing.T) {
	spots := map[string][2]float64{"near": {17.2, 19.4}, "middle": {18, 20.6}, "far": {18.6, 20}, "behind": {13, 20}}
	order := []string{"near", "middle", "far", "behind"}
	arena := func() (*mobs.Arena, map[string]uint64) {
		var spawns []gamedata.Spawn
		for _, name := range order {
			spawns = append(spawns, spawn("green_slime", 1, spots[name][0], spots[name][1], 0.01))
		}
		a := newArena(t, meadow(t, open(), spawns...), worldtest.FixedRoll{V: 0.5})
		ids := map[string]uint64{}
		for _, v := range a.Views() {
			for name, at := range spots {
				if math.Hypot(v.X-at[0], v.Z-at[1]) < 0.1 {
					ids[name] = v.ID
				}
			}
		}
		if len(ids) != len(spots) {
			t.Fatalf("slimes at %v; want one at each of %v", a.Views(), spots)
		}
		return a, ids
	}
	shot := gamedata.Shape{Kind: gamedata.ShotShape, Range: 6, Width: 0.5}
	area := gamedata.Shape{Kind: gamedata.AreaShape, Radius: 3}
	for _, tt := range []struct {
		name   string
		shape  *gamedata.Shape // nil is a Warrior's swing
		most   int
		target string // "" is none; "unknown" is an id no mob has
		want   []string
	}{
		{"a single-target swing at the far one", nil, 1, "far", []string{"far"}},
		{"a swing for two at the far one, then the nearest", nil, 2, "far", []string{"far", "near"}},
		{"a swing with no target", nil, 1, "", []string{"near"}},
		{"a swing at an unknown mob", nil, 1, "unknown", []string{"near"}},
		{"a swing at a mob behind, out of its arc", nil, 1, "behind", []string{"near"}},
		{"an area at the far one", &area, 1, "far", []string{"far"}},
		{"a shot at the far one past a nearer one in its lane", &shot, 1, "far", []string{"far"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, ids := arena()
			s := swing(16, 20, math.Pi/2)
			if tt.shape != nil {
				s.Shape = *tt.shape
			}
			s.Most = tt.most
			switch tt.target {
			case "":
			case "unknown":
				s.Target = 9999
			default:
				s.Target = ids[tt.target]
			}
			var got []string
			for _, b := range a.Strike(s) {
				for name, id := range ids {
					if id == b.Damage.Target.Mob {
						got = append(got, name)
					}
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("hit %v; want %v", got, tt.want)
			}
		})
	}
}

func TestChangesTellEachChangeOnce(t *testing.T) {
	a := alone(t, "green_slime")
	if changed, _ := a.Changes(); len(changed) != 1 {
		t.Fatalf("changed %+v; want its spawn", changed)
	}
	if changed, removed := a.Changes(); len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("changed %+v, removed %v; want nothing told twice", changed, removed)
	}
	weak := swing(20, 18, 0)
	weak.Power = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 10, Accuracy: 100}
	hit, _ := one(t, a.Strike(weak))
	a.Step(1, nil, nil)
	if changed, _ := a.Changes(); len(changed) != 1 || changed[0].ID != hit.Damage.Target.Mob || changed[0].HP == changed[0].MaxHP {
		t.Fatalf("changed %+v; want the slime hit, once", changed)
	}
}

// A tick nobody saw keeps its blockers, as a frame's do, and leaves no change for the next frame to tell.
func TestAnUnwatchedTickKeepsItsBlockersAndTellsNothingLater(t *testing.T) {
	a := slimes(t, 0.5)
	_, _ = a.Changes()
	weak := swing(20, 17, 0)
	weak.Power = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 10, Accuracy: 100}
	a.Strike(weak)
	a.Step(1, nil, nil)
	a.Unwatched()
	got, views := blockersAt(a, 1), a.Views()
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	if len(got) != 2 || got[1] != (sim.Blocker{X: views[1].X, Z: views[1].Z, Radius: slime.Radius, Bottom: 0, Top: slime.Height}) {
		t.Fatalf("tick 1 kept %+v; want both slimes as they stood", got)
	}
	if changed, removed := a.Changes(); len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("changed %+v, removed %v; want the unwatched tick's changes left untold", changed, removed)
	}
}

// blockersAt are the blockers tick t's frame kept, as a step reads them; nil for none.
func blockersAt(a *mobs.Arena, t uint64) []sim.Blocker {
	return sim.Geometry{}.Blocking(a.BlockersAt(t)).Blockers
}

func TestEachFramesBlockersAreKeptForHistoryTicks(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 1, 20, 20, 3)), &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25}})
	kept := map[uint64]sim.Blocker{}
	for n := uint64(1); n <= 3*mobs.History; n++ {
		a.Step(n, nil, nil)
		_, _ = a.Changes()
		kept[n] = blockersAt(a, n)[0]
		for back := range uint64(mobs.History) {
			if got := blockersAt(a, n-back); n > back && (len(got) != 1 || got[0] != kept[n-back]) {
				t.Fatalf("tick %d: tick %d's blockers %+v; want %+v", n, n-back, got, kept[n-back])
			}
		}
		if n > mobs.History && a.BlockersAt(n-mobs.History) != nil {
			t.Fatalf("tick %d: tick %d's blockers still kept", n, n-mobs.History)
		}
	}
	if a.BlockersAt(3*mobs.History+1) != nil {
		t.Fatal("a tick not yet sent has blockers")
	}
}

// king is the forest's boss, with its spot: a circle 3 m round (30, 30).
func king(t *testing.T) (gamedata.Mob, gamedata.BossSpot) {
	t.Helper()
	def, ok := worldtest.LoadData(t).Mob("king_slime")
	if !ok || !def.Boss {
		t.Fatal("no boss king_slime in the game data")
	}
	return def, gamedata.BossSpot{Mob: def.ID, At: gamedata.Point{X: 30, Z: 30}, Radius: 3}
}

// A boss stands at its spot's centre, wanders the spot's circle, and once killed stays gone while the slimes come back.
func TestABossSpawnsAtItsSpotsCentreAndNeverRespawns(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 2, 10, 10, 2)), &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25}})
	def, spot := king(t)
	id := a.SpawnBoss(def, spot)
	changed, _ := a.Changes()
	boss := changed[len(changed)-1]
	if boss.ID != id || boss.Type != "king_slime" || boss.X != 30 || boss.Y != 0 || boss.Z != 30 || boss.HP != def.HP || boss.MaxHP != def.HP {
		t.Fatalf("spawned %+v; want the king at its spot's centre, at full HP", boss)
	}
	moved := false
	for n := range uint64(400) {
		a.Step(n+1, nil, nil)
		for _, v := range a.Views() {
			if v.ID == id && distance(v, 30, 30) > spot.Radius+def.Radius {
				t.Fatalf("tick %d: the king at (%.2f, %.2f), outside its spot", n+1, v.X, v.Z)
			}
			moved = moved || v.ID == id && distance(v, 30, 30) > 0.5
		}
	}
	if !moved {
		t.Fatal("the king never wandered")
	}
	at := a.Views()[2]
	killer := swing(at.X, at.Z-def.Radius-1, 0)
	killer.Power = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1e6, Accuracy: 100}
	if blow, _ := one(t, a.Strike(killer)); blow.Killed == nil || blow.Damage.Target.Mob != id {
		t.Fatalf("blow %+v; want the king killed", blow)
	}
	for n := uint64(401); n < 401+uint64(time.Minute/tick); n++ {
		a.Step(n, nil, nil)
	}
	if views := a.Views(); len(views) != 2 || views[0].Type != "green_slime" || views[1].Type != "green_slime" {
		t.Fatalf("a minute on: %+v; want the two slimes and no king", views)
	}
}

// A blow's Dealt is the HP it took: all of a hit that leaves the mob alive, only what was left of it on a kill, none on a MISS.
func TestABlowDealsOnlyTheHPItTakes(t *testing.T) {
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	weak := swing(20, 18, 0)
	weak.Power = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 10, Accuracy: 100}
	a := alone(t, "green_slime")
	first, _ := one(t, a.Strike(weak))
	if first.Killed != nil || first.Damage.Amount == 0 || first.Dealt != first.Damage.Amount {
		t.Fatalf("blow %+v; want a hit that leaves the slime alive, dealing all of it", first)
	}
	last, _ := one(t, a.Strike(swing(20, 18, 0)))
	if last.Killed == nil || last.Damage.Amount <= slime.HP-first.Dealt || last.Dealt != slime.HP-first.Dealt {
		t.Fatalf("blow %+v after %d; want the kill to deal the %d HP left", last, first.Dealt, slime.HP-first.Dealt)
	}
	missing := slimes(t, 0.995) // past the best hit chance
	v := missing.Views()[0]
	if miss, _ := one(t, missing.Strike(swing(v.X, v.Z-1.5, 0))); !miss.Damage.Miss || miss.Dealt != 0 {
		t.Fatalf("blow %+v; want a MISS dealing nothing", miss)
	}
}

// stroller is a town with one slime at (20, 17), which walks north from tick 1 to a spot at (20, 23): in a town mobs only wander.
func stroller(t *testing.T) *mobs.Arena {
	t.Helper()
	m := meadow(t, open(), spawn("green_slime", 1, 20, 20, 3))
	m.Kind = gamedata.Town
	return newArena(t, m, &cycle{v: []float64{0.5, 1, 0.5, 0.5, 0, 1}}) // its spawn's point and yaw and pace, then its first spot
}

// A slime walking into a player stops touching their side, never in them, and walks on once they've gone.
func TestAMobStopsAtAPlayerItWouldWalkInto(t *testing.T) {
	a := stroller(t)
	p := player(20, 20)
	touched := false
	for n := uint64(1); n <= 100; n++ {
		a.Step(n, []mobs.Body{p}, nil)
		d := distance(a.Views()[0], p.X, p.Z)
		if d < 0.875-1e-9 {
			t.Fatalf("tick %d: the slime %.3f m off the player, in them", n, d)
		}
		touched = touched || d < 0.875+1e-9
	}
	if !touched {
		t.Fatal("the slime never stopped at the player's side")
	}
	for n := uint64(101); n <= 200; n++ {
		a.Step(n, nil, nil)
	}
	if v := a.Views()[0]; v.Z < 21 {
		t.Fatalf("the slime at (%.3f, %.3f); want it walked on north once the player left", v.X, v.Z)
	}
}

// A slime partly inside a player, as lag leaves it, goes no deeper, and walks on once they've gone.
func TestAMobPartlyInAPlayerGoesNoDeeper(t *testing.T) {
	a := stroller(t)
	p := player(20, 17.8) // 0.8 m off, 7.5 cm into the slime
	for n := uint64(1); n <= 30; n++ {
		a.Step(n, []mobs.Body{p}, nil)
		if d := distance(a.Views()[0], p.X, p.Z); d < 0.8-1e-9 {
			t.Fatalf("tick %d: the slime %.3f m off the player; want no deeper than 0.8", n, d)
		}
	}
	for n := uint64(31); n <= 200; n++ {
		a.Step(n, nil, nil)
	}
	if v := a.Views()[0]; v.Z < 19 {
		t.Fatalf("the slime at (%.3f, %.3f); want it walked on north once the player left", v.X, v.Z)
	}
}

// Two slimes on one spot that a player steps onto step out to their side apart, not one on the other.
func TestMobsAPlayerStepsIntoStepOutSideBySide(t *testing.T) {
	m := meadow(t, open(), spawn("green_slime", 2, 20, 20, 2))
	m.Kind = gamedata.Town
	a := newArena(t, m, worldtest.FixedRoll{V: 0.5}) // both on (20, 20 - √2), staying
	p := player(20, 20-math.Sqrt2+0.3)
	a.Step(1, []mobs.Body{p}, nil)
	v := a.Views()
	for _, s := range v {
		if d := distance(s, p.X, p.Z); d < playerRadius+0.5-1e-6 {
			t.Fatalf("a slime %.3f m from the player; want it out of them, %v or more", d, playerRadius+0.5)
		}
	}
	if d := distance(v[0], v[1].X, v[1].Z); d < 1-1e-6 {
		t.Fatalf("the slimes %.3f m apart; want them side by side, 1 m or more", d)
	}
}

// A slime with a player wholly inside it, as after they came down on it, steps out of them in its first tick and walks on: a player can't pin a mob.
func TestAMobWithAPlayerWhollyInsideStepsOutAndWalksOn(t *testing.T) {
	a := stroller(t)
	p := player(20, 17) // on the slime's centre
	a.Step(1, []mobs.Body{p}, nil)
	if d := distance(a.Views()[0], p.X, p.Z); d < playerRadius+0.5-1e-9 {
		t.Fatalf("the slime %.3f m from the player after a tick; want it out of them, %v or more", d, playerRadius+0.5)
	}
	for n := uint64(2); n <= 30; n++ {
		a.Step(n, []mobs.Body{p}, nil)
	}
	if d := distance(a.Views()[0], p.X, p.Z); d < 1 {
		t.Fatalf("the slime %.3f m from the player after 30 ticks; want it walked on", d)
	}
}
