package mobs_test

import (
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

const tick = 50 * time.Millisecond

// A slime area on a long platform: slimes are 32 px wide, so they patrol from x 116 to 284 and spawn, at a roll of 0.5, at 200.
const (
	minX, spawnX, maxX = 116.0, 200.0, 284.0
	floor              = 400.0
)

func slimes(respawn time.Duration) *gamedata.Map {
	return &gamedata.Map{
		ID: "meadow", Kind: gamedata.Hunting,
		Geometry: sim.Geometry{Platforms: []model.Rect{{X: 0, Y: floor, W: 1000, H: 16}}},
		Spawns:   []gamedata.Spawn{{Mob: "green_slime", Max: 2, Respawn: respawn, Area: model.Rect{X: 100, Y: 300, W: 200, H: 100}}},
	}
}

func newArena(t *testing.T, roll float64) *mobs.Arena {
	t.Helper()
	return mobs.NewArena(mobs.Deps{Map: slimes(5 * time.Second), Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: roll}}, mobs.Tuning{TickInterval: tick})
}

// strong hits whatever it reaches, for more than a slime's HP.
var strong = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1000, Accuracy: 100}

func meleeAt(x float64, facing int) mobs.Strike {
	return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.MeleeShape, Width: 60, Height: 60}, Most: 1, Power: strong, X: x, Y: floor, Facing: facing, Height: 60}
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

func TestTheArenaIsFullBeforeItsFirstTick(t *testing.T) {
	views := newArena(t, 0.5).Views()
	if len(views) != 2 || views[0].ID == views[1].ID {
		t.Fatalf("views %+v; want the area's two slimes", views)
	}
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	for _, v := range views {
		if v.Type != "green_slime" || v.X != spawnX || v.Y != floor || v.HP != slime.HP || v.MaxHP != slime.HP || v.Facing != 1 {
			t.Fatalf("spawned %+v", v)
		}
	}
}

// A slime walks to its platform's edge within the area, waits there a second, then turns and walks to the other edge.
func TestAMobPatrolsAndPausesAtEachEdge(t *testing.T) {
	a := newArena(t, 0.5)
	var xs []float64
	var facings []int
	for n := range uint64(200) {
		a.Step(n + 1)
		v := a.Views()[0]
		xs, facings = append(xs, v.X), append(facings, v.Facing)
	}
	pause := int(time.Second / tick)
	at := 0
	for _, edge := range []struct {
		x      float64
		facing int
	}{{maxX, -1}, {minX, 1}} {
		for at < len(xs) && xs[at] != edge.x {
			at++
		}
		if at+pause >= len(xs) {
			t.Fatalf("never reached %v: %v", edge.x, xs)
		}
		for k := at; k < at+pause; k++ {
			if xs[k] != edge.x || facings[k] != edge.facing {
				t.Fatalf("tick %d at %v facing %d; want a second's pause at %v facing %d", k+1, xs[k], facings[k], edge.x, edge.facing)
			}
		}
		if moved := xs[at+pause] - edge.x; moved*float64(edge.facing) <= 0 {
			t.Fatalf("after the pause it went from %v to %v; want back the way it now faces", edge.x, xs[at+pause])
		}
		at += pause
	}
}

func TestAKilledMobComesBackAfterItsAreasRespawn(t *testing.T) {
	a := newArena(t, 0.5)
	a.Step(1)
	_, _ = a.Changes()
	stood := a.Views()[0]
	blow, reached := one(t, a.Strike(meleeAt(stood.X-20, 1)))
	if !reached || blow.Killed == nil || blow.Killed.Mob.ID != "green_slime" || blow.Killed.X != stood.X || blow.Killed.Y != floor {
		t.Fatalf("blow %+v reached %v; want the slime killed where it stood", blow, reached)
	}
	if _, removed := a.Changes(); len(removed) != 1 || removed[0] != blow.Damage.Target.Mob {
		t.Fatalf("removed %v; want the killed slime", removed)
	}
	back := 1 + uint64(5*time.Second/tick)
	for n := uint64(2); n < back; n++ {
		a.Step(n)
		if got := len(a.Views()); got != 1 {
			t.Fatalf("tick %d: %d slimes; want one until the respawn", n, got)
		}
	}
	a.Step(back)
	views := a.Views()
	if len(views) != 2 || views[1].ID == blow.Damage.Target.Mob {
		t.Fatalf("tick %d: %+v; want a new slime back", back, views)
	}
}

func TestAStrikeReachesOnlyWhatItsAttackCovers(t *testing.T) {
	ranged := func(x float64, rng float64) mobs.Strike {
		return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.RangedShape, Range: rng}, Most: 1, Power: strong, X: x, Y: floor, Facing: 1, Height: 40}
	}
	tests := []struct {
		name    string
		strike  mobs.Strike
		reached bool
	}{
		{"a swing in front of the slime", meleeAt(spawnX-20, 1), true},
		{"a swing facing away", meleeAt(spawnX-20, -1), false},
		{"a swing out of reach", meleeAt(spawnX-100, 1), false},
		{"a shot in range", ranged(0, 300), true},
		{"a shot that falls short", ranged(0, 150), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blow, reached := one(t, newArena(t, 0.5).Strike(tt.strike))
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
	a := newArena(t, 0.995) // past the best hit chance
	blow, reached := one(t, a.Strike(meleeAt(a.Views()[0].X-20, 1)))
	if !reached || !blow.Damage.Miss || blow.Damage.Amount != 0 || blow.Killed != nil {
		t.Fatalf("blow %+v reached %v; want a miss", blow, reached)
	}
	for _, v := range a.Views() {
		if v.HP != v.MaxHP {
			t.Fatalf("a slime lost HP to a miss: %+v", v)
		}
	}
}

func TestTouchHurtsAPlayerOverlappingAMob(t *testing.T) {
	a := newArena(t, 0.5)
	id := kernel.NewCharacterID()
	dealt, mobX, touched := a.Touch(id, model.Rect{X: spawnX, Y: floor - 50, W: 30, H: 50}, model.Defender{})
	if !touched || mobX != spawnX || dealt.Target.Player != id || !dealt.Source.IsMob() || dealt.Amount == 0 {
		t.Fatalf("dealt %+v from %v touched %v; want the slime's hit", dealt, mobX, touched)
	}
	if _, _, touched := a.Touch(id, model.Rect{X: 600, Y: floor - 50, W: 30, H: 50}, model.Defender{}); touched {
		t.Fatal("a player far from every slime was touched")
	}
}

// A mob's touch meets the defense of its damage type: a slime's physical, a bat's magic.
func TestATouchMeetsTheDefenseOfTheMobsDamageType(t *testing.T) {
	touch := func(mob string, d model.Defender) int {
		t.Helper()
		m := slimes(time.Hour)
		m.Spawns[0].Mob, m.Spawns[0].Max = mob, 1
		a := mobs.NewArena(mobs.Deps{Map: m, Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
		v := a.Views()[0]
		dealt, _, touched := a.Touch(kernel.NewCharacterID(), model.Rect{X: v.X - 15, Y: v.Y - 20, W: 30, H: 20}, d)
		if !touched || dealt.Miss {
			t.Fatalf("%s: dealt %+v touched %v", mob, dealt, touched)
		}
		return dealt.Amount
	}
	walls := model.Defender{PhysicalDefense: 100, MagicDefense: 100}
	// Bat attack 22: 22·100/200 = 11 against either 100; a slime's 6: 6·100/200 = 3.
	for _, c := range []struct {
		mob        string
		d          model.Defender
		want       int
		meaningFor string
	}{
		{"cave_bat", model.Defender{PhysicalDefense: 100}, 22, "a bat ignores physical defense"},
		{"cave_bat", walls, 11, "a bat meets magic defense"},
		{"green_slime", model.Defender{MagicDefense: 100}, 6, "a slime ignores magic defense"},
		{"green_slime", walls, 3, "a slime meets physical defense"},
	} {
		if got := touch(c.mob, c.d); got != c.want {
			t.Errorf("%s: %d damage, want %d", c.meaningFor, got, c.want)
		}
	}
}

// A magic strike meets a mob's magic defense: a slime's is 1, its physical 2.
func TestAMagicStrikeMeetsAMobsMagicDefense(t *testing.T) {
	a := newArena(t, 0.5)
	s := meleeAt(spawnX-20, 1)
	s.Power = model.Attacker{Damage: gamedata.Magic, MagicAttack: 30.3, PhysicalAttack: 1000, Accuracy: 100}
	blow, reached := one(t, a.Strike(s))
	// 30.3·100/101 = 30
	if !reached || blow.Damage.Amount != 30 || blow.Killed != nil {
		t.Fatalf("blow %+v reached %v; want 30 against magic defense 1", blow, reached)
	}
}

// A strike for several hits each mob its shape reaches, nearest first, up to its most; an area reaches around the attacker.
func TestAStrikeHitsUpToItsMostNearestFirst(t *testing.T) {
	area := func(most int) mobs.Strike {
		return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 80}, Most: most, Power: strong, X: spawnX, Y: floor, Facing: 1, Height: 40}
	}
	if blows := newArena(t, 0.5).Strike(area(3)); len(blows) != 2 || blows[0].Killed == nil || blows[1].Killed == nil {
		t.Fatalf("blows %+v; want both slimes, killed", blows)
	}
	a := newArena(t, 0.5)
	blows := a.Strike(area(1))
	if len(blows) != 1 || len(a.Views()) != 1 {
		t.Fatalf("blows %+v, %d left; want one slime hit and one left", blows, len(a.Views()))
	}
	far := area(3)
	far.X = spawnX + 400
	if blows := newArena(t, 0.5).Strike(far); len(blows) != 0 {
		t.Fatalf("blows %+v; want none beyond the radius", blows)
	}
}

func TestChangesTellEachChangeOnce(t *testing.T) {
	a := newArena(t, 0.5)
	if changed, _ := a.Changes(); len(changed) != 2 {
		t.Fatalf("changed %+v; want both spawns", changed)
	}
	if changed, removed := a.Changes(); len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("changed %+v, removed %v; want nothing told twice", changed, removed)
	}
	a.Step(1)
	if changed, _ := a.Changes(); len(changed) != 2 || changed[0].X == spawnX {
		t.Fatalf("changed %+v; want both slimes moved", changed)
	}
}

// king is a boss on the slimes' platform, with an area 400 px wide around x 600.
func king(t *testing.T) (gamedata.Mob, model.Rect) {
	t.Helper()
	def, ok := worldtest.LoadData(t).Mob("king_slime")
	if !ok || !def.Boss {
		t.Fatal("no boss king_slime in the game data")
	}
	return def, model.Rect{X: 400, Y: floor - 128, W: 400, H: 128}
}

// A boss stands at its area's bottom centre, patrols its ground there, and once killed stays gone while the slimes come back.
func TestABossSpawnsAtItsAreasBottomCentreAndNeverRespawns(t *testing.T) {
	a := newArena(t, 0.5)
	def, area := king(t)
	id := a.SpawnBoss(def, area)
	changed, _ := a.Changes()
	boss := changed[len(changed)-1]
	if boss.ID != id || boss.Type != "king_slime" || boss.X != 600 || boss.Y != floor || boss.HP != def.HP || boss.MaxHP != def.HP {
		t.Fatalf("spawned %+v; want the king at its area's bottom centre, at full HP", boss)
	}
	for n := range uint64(400) {
		a.Step(n + 1)
		for _, v := range a.Views() {
			if v.ID == id && (v.X < area.X+def.Width/2 || v.X > area.X+area.W-def.Width/2) {
				t.Fatalf("tick %d: the king at x %v, outside its area", n+1, v.X)
			}
		}
	}
	at := a.Views()[2]
	killer := mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.MeleeShape, Width: 60, Height: 60}, Most: 1, Power: model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1e6, Accuracy: 100}, X: at.X - def.Width/2 - 10, Y: floor, Facing: 1, Height: 60}
	if blow, _ := one(t, a.Strike(killer)); blow.Killed == nil || blow.Damage.Target.Mob != id {
		t.Fatalf("blow %+v; want the king killed", blow)
	}
	for n := uint64(401); n < 401+uint64(time.Minute/tick); n++ {
		a.Step(n)
	}
	if views := a.Views(); len(views) != 2 || views[0].Type != "green_slime" || views[1].Type != "green_slime" {
		t.Fatalf("a minute on: %+v; want the two slimes and no king", views)
	}
}

// Remove takes a mob away without a kill, reported as gone once; a mob that isn't there is refused.
func TestRemoveTakesABossAwayWithoutAKill(t *testing.T) {
	a := newArena(t, 0.5)
	def, area := king(t)
	id := a.SpawnBoss(def, area)
	_, _ = a.Changes()
	if !a.Remove(id) {
		t.Fatal("the king was not removed")
	}
	if _, removed := a.Changes(); len(removed) != 1 || removed[0] != id {
		t.Fatalf("removed %v; want the king", removed)
	}
	if a.Remove(id) || len(a.Views()) != 2 {
		t.Fatalf("removed the king twice, or the slimes went with it: %+v", a.Views())
	}
}

// A blow's Dealt is the HP it took: all of a hit that leaves the mob alive, only what was left of it on a kill, none on a MISS.
func TestABlowDealsOnlyTheHPItTakes(t *testing.T) {
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	weak := meleeAt(spawnX-20, 1)
	weak.Power = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 10, Accuracy: 100}
	a := newArena(t, 0.5)
	first, _ := one(t, a.Strike(weak))
	if first.Killed != nil || first.Damage.Amount == 0 || first.Dealt != first.Damage.Amount {
		t.Fatalf("blow %+v; want a hit that leaves the slime alive, dealing all of it", first)
	}
	last, _ := one(t, a.Strike(meleeAt(spawnX-20, 1)))
	if last.Killed == nil || last.Damage.Amount <= slime.HP-first.Dealt || last.Dealt != slime.HP-first.Dealt {
		t.Fatalf("blow %+v after %d; want the kill to deal the %d HP left", last, first.Dealt, slime.HP-first.Dealt)
	}
	missing := newArena(t, 0.995) // past the best hit chance
	if miss, _ := one(t, missing.Strike(meleeAt(missing.Views()[0].X-20, 1))); !miss.Damage.Miss || miss.Dealt != 0 {
		t.Fatalf("blow %+v; want a MISS dealing nothing", miss)
	}
}
