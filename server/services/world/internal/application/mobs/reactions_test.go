package mobs_test

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// tap is a blow from x facing right, too weak to kill, that always lands.
func tap(x float64) mobs.Strike {
	s := meleeAt(x, 1)
	s.Power = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1, Accuracy: 100}
	return s
}

// pinned keeps the slime where it spawns: its ground is its own width.
func pinned(m *gamedata.Map) {
	m.Geometry.Platforms = []model.Rect{{X: spawnX - 16, Y: floor, W: 32, H: 16}}
	penned(32)(m)
}

// penned narrows the patrol area to w wide around spawnX, on the long floor: a mob of that width patrols nowhere but can still go after a player.
func penned(w float64) func(*gamedata.Map) {
	return func(m *gamedata.Map) { m.Spawns[0].Area = model.Rect{X: spawnX - w/2, Y: 300, W: w, H: 100} }
}

func poseOf(t *testing.T, a *mobs.Arena) model.MobView {
	t.Helper()
	views := a.Views()
	if len(views) != 1 {
		t.Fatalf("%d mobs, want 1", len(views))
	}
	return views[0]
}

// C12: a blow that lands stops a mob for 300 ms and pushes it 8 px away from the attacker; its windup ends, and that attack waits out its cooldown as if used.
func TestABlowFlinchesAMobAndCancelsItsWindup(t *testing.T) {
	a := alone(t, "green_slime")
	p := standing(spawnX - 40) // in its hop's reach, on its left
	if got := play(a, 1, 1+wait, still(p)); len(got.started) != 1 {
		t.Fatalf("started %+v; want the hop's windup", got.started)
	}
	if b, _ := one(t, a.Strike(tap(spawnX-20))); b.Damage.Miss || b.Killed != nil {
		t.Fatalf("blow %+v; want a hit the slime survives", b)
	}
	if v := poseOf(t, a); v.Pose != model.Flinching || v.X != spawnX+8 {
		t.Fatalf("slime %+v; want it flinching, pushed to %v", v, spawnX+8)
	}
	play(a, 2+wait, 7+wait, still(p))
	if v := poseOf(t, a); v.Pose != model.Flinching || v.X != spawnX+8 {
		t.Fatalf("slime %+v; want it still flinching where it was pushed, its 6th turn held", v)
	}
	got := play(a, 8+wait, 50+wait, still(p))
	if v := poseOf(t, a); v.Pose != model.Standing {
		t.Fatalf("slime %+v; want it standing again after 300 ms", v)
	}
	if len(got.started) != 0 || len(got.landed) != 0 {
		t.Fatalf("started %+v, landed at %v; want the cancelled hop to land nothing and wait out its 2.5 s", got.started, got.landedTicks())
	}
	if got := play(a, 51+wait, 51+wait, still(p)); len(got.started) != 1 {
		t.Fatalf("started %+v; want the hop again once its cooldown from its first windup ran out", got.started)
	}
}

// C12: a pose goes out in the frame it changes, even one where the mob neither moves nor is hurt, as when a flinch ends.
func TestAPoseChangeGoesOutInItsFrame(t *testing.T) {
	a := alone(t, "green_slime", pinned)
	a.Step(1, nil)
	one(t, a.Strike(tap(spawnX-20)))
	if changed, _ := a.Changes(); len(changed) != 1 || changed[0].Pose != model.Flinching {
		t.Fatalf("frame 1 sent %+v; want the slime flinching", changed)
	}
	for n := uint64(2); n <= 7; n++ {
		a.Step(n, nil)
		if changed, _ := a.Changes(); len(changed) != 0 {
			t.Fatalf("frame %d sent %+v; want nothing while it stands flinching", n, changed)
		}
	}
	a.Step(8, nil)
	if changed, _ := a.Changes(); len(changed) != 1 || changed[0].Pose != model.Standing {
		t.Fatalf("frame 8 sent %+v; want the slime standing again after its 6 turns", changed)
	}
}

// C12: a MISS neither flinches nor pushes a mob.
func TestAMissDoesNotFlinch(t *testing.T) {
	a := alone(t, "green_slime", pinned)
	s := tap(spawnX - 20)
	s.Power.Accuracy = 0
	a.Step(1, nil)
	for range 20 {
		if b, _ := one(t, a.Strike(s)); !b.Damage.Miss {
			continue
		}
		if v := poseOf(t, a); v.Pose != model.Standing || v.X != spawnX {
			t.Fatalf("slime %+v after a MISS; want it standing where it was", v)
		}
		return
	}
	t.Fatal("no MISS in 20 blows at accuracy 0")
}

// C12: a push stops where the mob would grow its overlap with a player, and at its ground's edge, but may take it out of its patrol area.
func TestAPushStopsAtAPlayerAndItsGround(t *testing.T) {
	a := alone(t, "green_slime")
	a.Step(1, []mobs.Body{standing(spawnX + 16 + 4 + 12)}) // their left edge 4 px past the slime's right
	one(t, a.Strike(tap(spawnX-20)))
	if v := poseOf(t, a); v.X != spawnX+4 {
		t.Fatalf("slime %+v; want it pushed 4 px, to the player", v)
	}
	a = alone(t, "green_slime", pinned)
	a.Step(1, nil)
	one(t, a.Strike(tap(spawnX-20)))
	if v := poseOf(t, a); v.X != spawnX || v.Pose != model.Flinching {
		t.Fatalf("slime %+v; want it flinching, held to its ground", v)
	}
	a = alone(t, "green_slime", penned(32))
	a.Step(1, nil)
	one(t, a.Strike(tap(spawnX-20)))
	if v := poseOf(t, a); v.X != spawnX+8 {
		t.Fatalf("slime %+v; want it pushed 8 px along its ground, out of its patrol area", v)
	}
}

// C12: three blows within 1.5 s of each other knock a mob down for 1 s; it rises in 1 s of super armor, and the count starts over.
func TestThreeBlowsKnockAMobDown(t *testing.T) {
	a := alone(t, "green_slime", pinned)
	p := standing(spawnX - 24) // overlapping the slime, so its hop reaches them though its ground holds it in place
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	play(a, 1, wait, still(p))                                   // its wait
	for _, n := range []uint64{1 + wait, 20 + wait, 49 + wait} { // 19 and 29 ticks apart: inside the 30
		if got := play(a, n, n, still(p)); n == 1+wait && len(got.started) != 1 {
			t.Fatalf("started %+v; want the hop's windup, which the first blow breaks off", got.started)
		}
		one(t, a.Strike(tap(spawnX-20)))
	}
	if v := poseOf(t, a); v.Pose != model.Down {
		t.Fatalf("slime %+v; want it down at the third blow", v)
	}
	hp := poseOf(t, a).HP
	one(t, a.Strike(tap(spawnX-20)))
	if v := poseOf(t, a); v.Pose != model.Down || v.HP >= hp {
		t.Fatalf("slime %+v after a blow while down; want it hurt and still down", v)
	}
	if got := play(a, 50+wait, 69+wait, still(p)); len(got.started) != 0 { // its hop ready 50 ticks after its first windup
		t.Fatalf("started %+v; want no attack in its 20 turns down", got.started)
	}
	if got := play(a, 70+wait, 70+wait, still(p)); len(got.started) != 1 || poseOf(t, a).Pose != model.Rising {
		t.Fatalf("slime %+v, started %+v; want it rising after 1 s down, and hopping", poseOf(t, a), got.started)
	}
	one(t, a.Strike(tap(spawnX-20)))
	if v := poseOf(t, a); v.Pose != model.Rising {
		t.Fatalf("slime %+v; want a blow while rising to leave it in its super armor", v)
	}
	play(a, 71+wait, 90+wait, still(p))
	if v := poseOf(t, a); v.Pose != model.Standing || v.HP <= 0 || v.HP > slime.HP {
		t.Fatalf("slime %+v; want it standing after its 1 s of super armor, its hop still under way", v)
	}
	one(t, a.Strike(tap(spawnX-20)))
	if v := poseOf(t, a); v.Pose != model.Flinching {
		t.Fatalf("slime %+v; want the count started over, the next blow a flinch", v)
	}
}

// C12: the down count starts over 1.5 s after the last blow.
func TestTheDownCountEndsAfterAPause(t *testing.T) {
	a := alone(t, "green_slime", pinned)
	for _, n := range []uint64{1, 10, 40} { // the third 30 ticks after the second
		play(a, n, n, still())
		one(t, a.Strike(tap(spawnX-20)))
	}
	if v := poseOf(t, a); v.Pose != model.Flinching {
		t.Fatalf("slime %+v; want a flinch, the count started over", v)
	}
}

// C12: a mob in super armor never flinches, is pushed or knocked down, and its attack goes on whatever hits it.
func TestSuperArmorTakesBlowsWithoutReacting(t *testing.T) {
	a := alone(t, "mossback_snail")
	p := standing(spawnX - 60)
	if got := play(a, 1, 1+wait, still(p)); len(got.started) != 1 {
		t.Fatalf("started %+v; want the charge's windup", got.started)
	}
	for range 4 {
		one(t, a.Strike(tap(spawnX-30)))
		if v := poseOf(t, a); v.Pose != model.Standing || v.X != spawnX {
			t.Fatalf("elite %+v; want it standing where it was", v)
		}
	}
	if got := play(a, 2+wait, 30+wait, still(p)); len(got.landed) != 1 {
		t.Fatalf("landed at %v; want its charge to land on the player", got.landedTicks())
	}
}

// C12: each mob walks at its own pace and pauses at an edge for a rolled while, so mobs a player herded into one spot drift apart once they're free.
func TestAHerdedStackDriftsApart(t *testing.T) {
	m := slimes(time.Hour)
	m.Kind, m.Spawns[0].Max = gamedata.Town, 5 // in a town mobs only patrol and stop at players
	a := mobs.NewArena(mobs.Deps{Map: m, Data: worldtest.LoadData(t), Roll: rand.New(rand.NewPCG(7, 11))}, mobs.Tuning{TickInterval: tick})
	herd := bodies(player(80)) // their right edge at 104, inside the slimes' ground: every slime ends against them at x 120
	for n := range uint64(600) {
		a.Step(n+1, herd)
	}
	if xs := xsOf(a); xs[len(xs)-1]-xs[0] > 0 {
		t.Fatalf("slimes at %v; want them herded into one spot", xs)
	}
	for n := uint64(601); n <= 900; n++ {
		a.Step(n, nil)
	}
	if xs := xsOf(a); xs[len(xs)-1]-xs[0] < 32 {
		t.Fatalf("slimes at %v 15 s after the player left; want them a slime's width apart or more", xs)
	}
}

func xsOf(a *mobs.Arena) []float64 {
	var xs []float64
	for _, v := range a.Views() {
		xs = append(xs, v.X)
	}
	slices.Sort(xs)
	return xs
}

// C12: a pause at an edge is rolled from 600 ms to 1400 ms: the mob walks on that many ticks after it got there.
func TestAnEdgePauseIsRolled(t *testing.T) {
	for _, tt := range []struct {
		roll  float64
		ticks int
	}{{0, 12}, {0.999, 28}} {
		a := mobs.NewArena(mobs.Deps{Map: slimes(time.Hour), Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: tt.roll}}, mobs.Tuning{TickInterval: tick})
		var atEdge, paused int
		for n := uint64(1); n <= 400 && paused == 0; n++ {
			before := a.Views()[0].X
			a.Step(n, nil)
			switch x := a.Views()[0].X; {
			case atEdge == 0 && (x == minX || x == maxX):
				atEdge = int(n)
			case atEdge > 0 && x != before:
				paused = int(n) - atEdge
			}
		}
		if paused != tt.ticks {
			t.Fatalf("at a roll of %v the slime paused %d ticks; want %d", tt.roll, paused, tt.ticks)
		}
	}
}

// C12: a blow during a hop's flight flinches the slime, but the hop flies on and lands; the flinch ends on time all the same.
func TestAnActUnderWayPlaysOut(t *testing.T) {
	a := alone(t, "green_slime")
	p := standing(140)
	play(a, 1, 15+wait, still(p)) // the windup from tick 1+wait, the flight from 13+wait
	one(t, a.Strike(tap(poseOf(t, a).X-20)))
	if v := poseOf(t, a); v.Pose != model.Flinching {
		t.Fatalf("slime %+v; want it flinching", v)
	}
	if got := play(a, 16+wait, 21+wait, still(p)); !slices.Equal(got.landedTicks(), []uint64{21 + wait}) {
		t.Fatalf("landed at %v; want the hop to land at tick %d as it would have", got.landedTicks(), 21+wait)
	}
	play(a, 22+wait, 22+wait, still(p))
	if v := poseOf(t, a); v.Pose != model.Standing {
		t.Fatalf("slime %+v; want its flinch over after 6 turns, though its hop recovers 5 ticks more", v)
	}
}

// C12: a knockdown ends a hop in flight: the slime stops where it is, lands nothing, and lies there its 1 s.
func TestAKnockdownEndsTheActUnderWay(t *testing.T) {
	a := alone(t, "green_slime")
	for _, n := range []uint64{1, 8} { // two blows, the second as the first's flinch ends
		play(a, n, n, still())
		one(t, a.Strike(tap(poseOf(t, a).X-20)))
	}
	p := standing(poseOf(t, a).X - 40) // in its hop's reach, seen once the second flinch ends at tick 15
	if got := play(a, 9, 28+wait, still(p)); len(got.started) != 1 || got.started[0].Tick != 15+wait {
		t.Fatalf("started %+v; want the hop's windup at tick %d, its wait after the second flinch, its flight from %d", got.started, 15+wait, 27+wait)
	}
	one(t, a.Strike(tap(poseOf(t, a).X-20))) // 24 ticks after the second blow
	x := poseOf(t, a).X
	if got := play(a, 29+wait, 48+wait, still(p)); len(got.landed) != 0 {
		t.Fatalf("landed at %v; want the hop the knockdown ended to land nothing", got.landedTicks())
	}
	if v := poseOf(t, a); v.Pose != model.Down || v.X != x {
		t.Fatalf("slime %+v; want it down where the blow found it, at %v", v, x)
	}
}
