package mobs_test

import (
	"math"
	"slices"
	"testing"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// nudge is a blow from (x, z) on the mob within 3 m, too weak to kill a slime, that lands at a roll of 0.5.
func nudge(x, z float64) mobs.Strike {
	return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 3}, Most: 1, Power: model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1, Accuracy: 100}, Aim: model.Aim{X: x, Z: z}}
}

// nudgeFromNorth is a nudge from 1 m north of the arena's one mob, pushing it south.
func nudgeFromNorth(t *testing.T, a *mobs.Arena) mobs.Blow {
	t.Helper()
	v := theMob(t, a)
	b, _ := one(t, a.Strike(nudge(v.X, v.Z+1)))
	if b.Damage.Miss || b.Killed != nil {
		t.Fatalf("blow %+v; want a hit the mob survives", b)
	}
	return b
}

// theMob is the arena's one mob.
func theMob(t *testing.T, a *mobs.Arena) model.MobView {
	t.Helper()
	views := a.Views()
	if len(views) != 1 {
		t.Fatalf("%d mobs, want 1", len(views))
	}
	return views[0]
}

// A blow that lands stops a mob for 300 ms and pushes it 0.25 m away from the attacker; it ends a windup, whose attack waits out its cooldown from the windup.
func TestABlowFlinchesAMobAndCancelsItsWindup(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	p := player(20, v.Z-3) // in its hop's reach, south
	at, next := windup(t, a, []mobs.Body{p}, 1)
	if b, _ := one(t, a.Strike(nudge(20, v.Z-1))); b.Damage.Miss || b.Killed != nil {
		t.Fatalf("blow %+v; want a hit the slime survives", b)
	}
	if got := theMob(t, a); got.Pose != model.Flinching || got.X != 20 || math.Abs(got.Z-(v.Z+0.25)) > 1e-9 {
		t.Fatalf("slime %+v; want it flinching, pushed north to z %v", got, v.Z+0.25)
	}
	flinch := stepThrough(a, next, next+5, p) // the flinch's 300 ms from the next tick
	if got := theMob(t, a); got.Pose != model.Flinching || got.X != 20 || math.Abs(got.Z-(v.Z+0.25)) > 1e-9 {
		t.Fatalf("slime %+v; want it still flinching where it was pushed", got)
	}
	stepThrough(a, next+6, next+6, p)
	if got := theMob(t, a); got.Pose != model.Standing {
		t.Fatalf("slime %+v; want it standing again after 300 ms", got)
	}
	cooling := stepThrough(a, next+7, at.Tick+49, p) // its hop's 2.5 s from the windup
	if len(flinch.startTicks())+len(cooling.startTicks()) != 0 || len(flinch.landTicks)+len(cooling.landTicks) != 0 {
		t.Fatalf("started at %v %v, landed at %v %v; want the cancelled hop to land nothing and cool", flinch.startTicks(), cooling.startTicks(), flinch.landTicks, cooling.landTicks)
	}
	if got := stepThrough(a, at.Tick+50, at.Tick+50, p); len(got.startTicks()) != 1 {
		t.Fatalf("started %v; want the hop again as its cooldown from the first windup ran out", got.startTicks())
	}
}

// A MISS neither flinches nor pushes a mob.
func TestAMissDoesNotFlinch(t *testing.T) {
	a := alone(t, "green_slime")
	a.Step(1, nil)
	v := theMob(t, a)
	s := nudge(20, v.Z-1)
	s.Power.Accuracy = 0 // the least hit chance, under a roll of 0.5
	if b, _ := one(t, a.Strike(s)); !b.Damage.Miss {
		t.Fatalf("blow %+v; want a MISS", b)
	}
	if got := theMob(t, a); got.Pose != model.Standing || got.X != v.X || got.Z != v.Z {
		t.Fatalf("slime %+v after a MISS; want it standing where it was", got)
	}
}

// A push stops where the mob meets a player, a solid too tall to step onto, or the map's edge; a mob still flinches where it stops.
func TestAPushStopsAtAPlayerASolidAndTheMapsEdge(t *testing.T) {
	z0 := 20 - 0.01*math.Sqrt(0.5) // where a slime spawned in a circle 0.01 m round (20, 20) stands at a roll of 0.5
	for _, c := range []struct {
		name    string
		g       sim.Geometry
		at      float64 // its spawn's z
		players []mobs.Body
		want    float64
	}{
		{"a player 0.1 m off its north edge", open(), 20, []mobs.Body{player(20, z0+0.5+playerRadius+0.1)}, z0 + 0.1},
		{"a wall 0.1 m off its north edge", open(sim.Box{X: 20, Z: z0 + 0.6 + 0.25, HalfW: 2, HalfD: 0.25, UX: 1, Top: 1}), 20, nil, z0 + 0.1},
		{"the map's north edge 0.1 m off", open(), 40 - 0.5 - 0.1 + 0.01*math.Sqrt(0.5), nil, 40 - 0.5},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newArena(t, meadow(t, c.g, spawn("green_slime", 1, 20, c.at, 0.01)), worldtest.FixedRoll{V: 0.5})
			a.Step(1, c.players) // the players are what the tick's pushes stop at
			v := theMob(t, a)
			one(t, a.Strike(nudge(20, v.Z-1)))
			if got := theMob(t, a); got.Pose != model.Flinching || got.X != 20 || math.Abs(got.Z-c.want) > 1e-9 {
				t.Fatalf("slime %+v; want it flinching, pushed to z %v", got, c.want)
			}
		})
	}
}

// Three blows each within 1.5 s of the last knock a mob down for 1 s, its attack ended; it rises with 1 s of super armor, and the count starts over.
func TestThreeBlowsKnockAMobDown(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	p := player(20, v.Z-0.5-playerRadius) // touching it, south, so its hop lands where it stands; blows from the north push it into them
	if got := stepThrough(a, 1, 5, p); !slices.Equal(got.startTicks(), []uint64{5}) {
		t.Fatalf("started at %v; want the hop's windup at tick 5, after its engage wait", got.startTicks())
	}
	nudgeFromNorth(t, a) // breaks off the windup
	stepThrough(a, 6, 24, p)
	nudgeFromNorth(t, a) // 19 ticks on
	stepThrough(a, 25, 53, p)
	nudgeFromNorth(t, a) // 29 ticks on, inside the 30
	down := theMob(t, a)
	if down.Pose != model.Down {
		t.Fatalf("slime %+v; want it down at the third blow", down)
	}
	nudgeFromNorth(t, a)
	if got := theMob(t, a); got.Pose != model.Down || got.HP >= down.HP || got.X != down.X || got.Z != down.Z {
		t.Fatalf("slime %+v after a blow while down; want it hurt, still down, not pushed", got)
	}
	if got := stepThrough(a, 54, 73, p); len(got.startTicks()) != 0 || theMob(t, a).Pose != model.Down {
		t.Fatalf("slime %+v, started at %v; want it down and still for its 20 ticks, its hop ready since tick 55", theMob(t, a), got.startTicks())
	}
	if got := stepThrough(a, 74, 74, p); len(got.startTicks()) != 1 || theMob(t, a).Pose != model.Rising {
		t.Fatalf("slime %+v, started at %v; want it rising after 1 s down, and hopping", theMob(t, a), got.startTicks())
	}
	rising := theMob(t, a)
	nudgeFromNorth(t, a)
	if got := theMob(t, a); got.Pose != model.Rising || got.HP >= rising.HP || got.X != rising.X || got.Z != rising.Z {
		t.Fatalf("slime %+v; want a blow while it rises to hurt it and leave it in its super armor", got)
	}
	if got := stepThrough(a, 75, 93, p); len(got.landTicks) != 0 || theMob(t, a).Pose != model.Rising {
		t.Fatalf("slime %+v, landed at %v; want it rising for 1 s, its hop under way", theMob(t, a), got.landTicks)
	}
	if got := stepThrough(a, 94, 94, p); !slices.Equal(got.landTicks, []uint64{94}) || theMob(t, a).Pose != model.Standing {
		t.Fatalf("slime %+v, landed at %v; want it standing as its hop lands", theMob(t, a), got.landTicks)
	}
	nudgeFromNorth(t, a)
	if got := theMob(t, a); got.Pose != model.Flinching {
		t.Fatalf("slime %+v; want the count started over, the next blow a flinch", got)
	}
}

// The down count starts over once 1.5 s pass without a blow: three blows with the third 30 ticks after the second only flinch.
func TestTheDownCountEndsAfterAPause(t *testing.T) {
	for _, c := range []struct {
		third uint64
		want  model.MobPose
	}{{39, model.Down}, {40, model.Flinching}} {
		a := alone(t, "green_slime")
		last := uint64(0)
		for _, n := range []uint64{1, 10, c.third} {
			stepThrough(a, last+1, n)
			nudgeFromNorth(t, a)
			last = n
		}
		if got := theMob(t, a); got.Pose != c.want {
			t.Fatalf("third blow at tick %d: slime %+v; want pose %v", c.third, got, c.want)
		}
	}
}

// A mob with super armor never flinches, is pushed or knocked down, and its attack goes on whatever hits it.
func TestSuperArmorTakesBlowsWithoutReacting(t *testing.T) {
	a := alone(t, "mossback_snail")
	v := theMob(t, a)
	p := player(20, v.Z-4) // in its charge's reach, south
	_, next := windup(t, a, []mobs.Body{p}, 1)
	for range 4 {
		nudgeFromNorth(t, a)
		if got := theMob(t, a); got.Pose != model.Standing || got.X != v.X || got.Z != v.Z {
			t.Fatalf("elite %+v; want it standing where it was", got)
		}
	}
	if hits := landed(a, []mobs.Body{p}, next, 24); len(hits) != 1 || hits[0].Player != p.ID { // the windup's 800 ms and the travel's 400
		t.Fatalf("hits %+v; want its charge to land on the player", hits)
	}
}

// A blow on a hop in flight flinches the slime, but the hop flies on and lands on time where it was aimed; the flinch ends on time all the same.
func TestAFlinchLeavesAnActUnderWayToPlayOut(t *testing.T) {
	a := alone(t, "green_slime")
	p := player(20, theMob(t, a).Z-3)
	at, next := windup(t, a, []mobs.Body{p}, 1) // its landing touching the player
	stepThrough(a, next, at.Tick+14, p)         // the windup's 12 ticks, then 2 of the travel's 8
	v := theMob(t, a)
	one(t, a.Strike(nudge(v.X+1, v.Z))) // from the east, pushing it off its line
	if got := theMob(t, a); got.Pose != model.Flinching {
		t.Fatalf("slime %+v; want it flinching", got)
	}
	if got := stepThrough(a, at.Tick+15, at.Tick+20, p); !slices.Equal(got.landTicks, []uint64{at.Tick + 20}) {
		t.Fatalf("landed at %v; want the hop to land at tick %d as it would have", got.landTicks, at.Tick+20)
	}
	if got := theMob(t, a); math.Abs(got.X-at.ToX) > 1e-9 || math.Abs(got.Z-at.ToZ) > 1e-9 {
		t.Fatalf("slime at (%v, %v); want it landed where the hop was aimed, (%v, %v)", got.X, got.Z, at.ToX, at.ToZ)
	}
	stepThrough(a, at.Tick+21, at.Tick+21, p)
	if got := theMob(t, a); got.Pose != model.Standing {
		t.Fatalf("slime %+v; want its flinch over after 300 ms, though its hop recovers 5 ticks more", got)
	}
}

// A knockdown ends a hop in flight: the slime stops where it is, lands nothing, and lies there its 1 s.
func TestAKnockdownEndsTheActUnderWay(t *testing.T) {
	a := alone(t, "green_slime")
	stepThrough(a, 1, 1)
	nudgeFromNorth(t, a)
	stepThrough(a, 2, 8)
	nudgeFromNorth(t, a)              // as the first's flinch ends
	p := player(20, theMob(t, a).Z-3) // in its hop's reach, taken once the second flinch ends at tick 15
	at, next := windup(t, a, []mobs.Body{p}, 9)
	if at.Tick != 19 {
		t.Fatalf("windup at tick %d; want 19, its engage wait after the second flinch", at.Tick)
	}
	if got := stepThrough(a, next, 33, p); len(got.landTicks) != 0 { // the windup to 31, then 2 ticks of flight
		t.Fatalf("landed at %v; want the hop in flight", got.landTicks)
	}
	nudgeFromNorth(t, a) // 25 ticks after the second blow
	down := theMob(t, a)
	if down.Pose != model.Down || math.Abs(down.Z-at.ToZ) < 0.1 {
		t.Fatalf("slime %+v; want it down short of the hop's landing at z %v", down, at.ToZ)
	}
	if got := stepThrough(a, 34, 53, p); len(got.landTicks) != 0 {
		t.Fatalf("landed at %v; want the hop the knockdown ended to land nothing", got.landTicks)
	}
	if got := theMob(t, a); got.Pose != model.Down || got.X != down.X || got.Z != down.Z {
		t.Fatalf("slime %+v; want it down where the blow found it, at (%v, %v)", got, down.X, down.Z)
	}
}

// A pose goes out in the frame it changes, even one in which the mob neither moves nor is hurt, as when a flinch ends.
func TestAPoseChangeGoesOutInItsFrame(t *testing.T) {
	a := alone(t, "green_slime") // it stands where it spawned, pausing for 1 s
	a.Step(1, nil)
	one(t, a.Strike(nudge(20, theMob(t, a).Z-1)))
	changed, _ := a.Changes()
	if len(changed) != 1 || changed[0].Pose != model.Flinching {
		t.Fatalf("frame 1 sent %+v; want the slime flinching", changed)
	}
	hit := changed[0]
	for n := uint64(2); n <= 7; n++ {
		a.Step(n, nil)
		if changed, _ := a.Changes(); len(changed) != 0 {
			t.Fatalf("frame %d sent %+v; want nothing while it stands flinching", n, changed)
		}
	}
	a.Step(8, nil)
	if changed, _ := a.Changes(); len(changed) != 1 || changed[0].Pose != model.Standing || changed[0].X != hit.X || changed[0].Z != hit.Z || changed[0].HP != hit.HP {
		t.Fatalf("frame 8 sent %+v; want the slime standing again where it was, its 300 ms over", changed)
	}
}

// Idle mobs that overlap drift apart at 1 m/s each, the later east of two on one point, until they stand side by side.
func TestIdleMobsThatOverlapDriftApart(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 2, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.999}) // both on one point, pausing 28 ticks
	apart := 0.0
	for n := uint64(1); n <= 28; n++ {
		a.Step(n, nil)
		views := a.Views()
		d := distance(views[0], views[1].X, views[1].Z)
		if d > apart+0.1+1e-9 || d < apart-1e-9 || views[1].X < views[0].X {
			t.Fatalf("tick %d: slimes at (%.3f, %.3f) and (%.3f, %.3f), %.3f m apart from %.3f; want them parting at most 0.1 m a tick, the later east", n, views[0].X, views[0].Z, views[1].X, views[1].Z, d, apart)
		}
		if apart = d; n >= 10 && math.Abs(d-1) > 1e-9 {
			t.Fatalf("tick %d: slimes %.6f m apart; want them side by side, 1 m, from tick 10", n, d)
		}
	}
}

// A slime hops onto one waiting at the player's side: the waiting one makes way while the hopper recovers, rather than standing in it, and they've parted within 1.75 s of the landing.
func TestAWaitingMobMakesWayForOneThatLandsOnIt(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 1, 20, 13.2, 0.01), spawn("green_slime", 1, 20, 17, 0.01)), worldtest.FixedRoll{V: 0.5})
	p := []mobs.Body{player(20, 10)} // the near slime hops to their side and waits there, the far one hops onto it
	overlap := func() float64 {
		v := a.Views()
		return 1 - distance(v[0], v[1].X, v[1].Z) // two slimes' radii
	}
	var landed uint64
	for n := uint64(1); landed == 0 || n <= landed; n++ {
		if n > 200 {
			t.Fatal("the far slime never hopped")
		}
		a.Step(n, p)
		for _, w := range a.Started() {
			if w.Mob == 2 && landed == 0 {
				landed = n + 20 // its 600 ms windup and 400 ms travel
			}
		}
	}
	at := overlap()
	if at < 0.5 {
		t.Fatalf("the far slime landed %.3f m into the near one; want it on top of it", at)
	}
	for n := landed + 1; n <= landed+3; n++ {
		a.Step(n, p)
	}
	if o := overlap(); o > at-0.01 {
		t.Fatalf("%.3f m into each other 150 ms into the hopper's recovery, from %.3f; want the waiting one making way", o, at)
	}
	for n := landed + 4; overlap() > 1e-6; n++ {
		if n > landed+35 {
			t.Fatalf("still %.3f m into each other 1.75 s after the landing; want them parted", overlap())
		}
		a.Step(n, p)
	}
}

// A wander's pause is rolled from 600 ms to 1400 ms: the mob sets off that many ticks after it got to its spot.
func TestAWanderPauseIsRolled(t *testing.T) {
	for _, c := range []struct {
		roll  float64
		ticks uint64
	}{{0, 12}, {0.999, 28}} {
		r := c.roll // the spawn rolls its spot, yaw and pace from these, then its wander rolls spots east and west, pausing at each
		a := newArena(t, meadow(t, open(), spawn("green_slime", 1, 20, 20, 3)), &cycle{v: []float64{r, 0.75, 0.99, r, 0.25, 0.99}})
		var arrived, paused uint64
		last := theMob(t, a)
		for n := uint64(1); n <= 400 && paused == 0; n++ {
			a.Step(n, nil)
			v := theMob(t, a)
			moved := v.X != last.X || v.Z != last.Z
			switch {
			case moved && arrived > 0:
				paused = n - arrived
			case moved:
				arrived = 0
			case arrived == 0 && n > 1:
				arrived = n - 1 // it got there on the last tick it moved
			}
			last = v
		}
		if paused != c.ticks {
			t.Fatalf("at a roll of %v the slime paused %d ticks; want %d", c.roll, paused, c.ticks)
		}
	}
}
