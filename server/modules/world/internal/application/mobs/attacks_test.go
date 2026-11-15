package mobs_test

import (
	"math"
	"testing"

	"finga/internal/sim"
	"finga/modules/world/internal/application/mobs"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

// windup steps a from tick from until a mob's windup starts, failing after 3 s, and returns it and the next tick.
func windup(t *testing.T, a *mobs.Arena, players []mobs.Body, from uint64) (model.MobAttacked, uint64) {
	t.Helper()
	for n := from; n < from+60; n++ {
		a.Step(n, players)
		if s := a.Started(); len(s) > 0 {
			return s[0], n + 1
		}
	}
	t.Fatal("no windup in 3 s")
	return model.MobAttacked{}, 0
}

// landed steps a from tick from through the attack begun before it, lasting ticks, and returns the hits of each landing, in order.
func landed(a *mobs.Arena, players []mobs.Body, from, ticks uint64) []mobs.Hit {
	var hits []mobs.Hit
	for n := from; n < from+ticks; n++ {
		a.Step(n, players)
		for _, l := range a.Landings() {
			hits = append(hits, l.Hits...)
		}
	}
	return hits
}

// A bite fixes its arc at the windup, turned to its target: it lands on them if they stay, knocking them away, and on nobody if they step out.
func TestABiteLandsOnlyOnAPlayerStillInItsArc(t *testing.T) {
	for _, stays := range []bool{true, false} {
		a := boarAt(t, open(), 20, 20)
		v := a.Views()[0]
		p := player(20, v.Z-1.3) // 5 cm short of touching, south
		at, next := windup(t, a, []mobs.Body{p}, 1)
		if at.ToX != v.X || at.ToZ != v.Z || math.Abs(math.Abs(float64(at.Yaw))-math.Pi) > 1e-3 {
			t.Fatalf("windup %+v; want it where the boar stands, turned south", at)
		}
		if !stays {
			p.Z -= 3
		}
		hits := landed(a, []mobs.Body{p}, next, 12) // the windup's 600 ms
		if !stays {
			if len(hits) != 0 {
				t.Fatalf("hits %+v; want none on a player who stepped out", hits)
			}
			continue
		}
		if len(hits) == 0 || hits[0].Player != p.ID || hits[0].DZ > -0.99 {
			t.Fatalf("hits %+v; want the player knocked south", hits)
		}
	}
}

// A hop's landing is fixed at the windup, touching its target: it lands there on them, and on nobody who stepped aside.
func TestAHopLandsTouchingWhereItsTargetStood(t *testing.T) {
	for _, stays := range []bool{true, false} {
		a := alone(t, "green_slime")
		v := a.Views()[0]
		p := player(20, v.Z-3)
		at, next := windup(t, a, []mobs.Body{p}, 1)
		if math.Abs(at.ToX-20) > 1e-6 || math.Abs(at.ToZ-(p.Z+0.875)) > 1e-6 { // its yaw is a float32
			t.Fatalf("windup %+v; want its landing touching the player at z %v", at, p.Z+0.875)
		}
		if !stays {
			p.X += 3
		}
		hits := landed(a, []mobs.Body{p}, next, 20) // the windup's 600 ms and the travel's 400
		if v := a.Views()[0]; math.Abs(v.X-at.ToX) > 1e-9 || math.Abs(v.Z-at.ToZ) > 1e-9 {
			t.Fatalf("the slime landed at (%v, %v); want (%v, %v)", v.X, v.Z, at.ToX, at.ToZ)
		}
		if got := len(hits) > 0 && hits[0].Player == p.ID; got != stays {
			t.Fatalf("hits %+v; want a hit %v", hits, stays)
		}
	}
}

// A charge runs its lane and ends on the first player it meets, stopping at their side; a wall cuts it short.
func TestAChargeEndsOnTheFirstPlayerItMeetsOrAWall(t *testing.T) {
	t.Run("two players in its lane", func(t *testing.T) {
		a := newArena(t, meadow(t, open(), spawn("orange_mushroom", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
		v := a.Views()[0]
		near, far := player(20, v.Z-2.5), player(20, v.Z-4)
		_, next := windup(t, a, []mobs.Body{near, far}, 1)
		hits := landed(a, []mobs.Body{near, far}, next, 24) // the windup's 800 ms and the travel's 400
		if len(hits) == 0 || hits[0].Player != near.ID {
			t.Fatalf("hits %+v; want the near player first", hits)
		}
		if v := a.Views()[0]; v.Z < near.Z+0.5625+playerRadius-1e-9 {
			t.Fatalf("the mushroom ran on to z %v, into the near player at %v", v.Z, near.Z)
		}
	})
	t.Run("a wall in its lane", func(t *testing.T) {
		wall := sim.Box{X: 20, Z: 18.5, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3}
		a := newArena(t, meadow(t, open(wall), spawn("orange_mushroom", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
		p := player(20, 17)
		at, next := windup(t, a, []mobs.Body{p}, 1)
		if math.Abs(at.ToZ-(18.75+0.5625)) > 1e-9 {
			t.Fatalf("windup %+v; want its end at the wall's face, z %v", at, 18.75+0.5625)
		}
		if hits := landed(a, []mobs.Body{p}, next, 24); len(hits) != 0 {
			t.Fatalf("hits %+v; want none through the wall", hits)
		}
	})
}

// A spit flies along its yaw and lands on the player it meets; a wall in its way stops it.
func TestASpitFliesToItsTargetUnlessAWallStopsIt(t *testing.T) {
	for _, walled := range []bool{false, true} {
		g := open()
		if walled {
			g = open(sim.Box{X: 20, Z: 17, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3})
		}
		a := newArena(t, meadow(t, g, spawn("cave_bat", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
		v := a.Views()[0]
		p := player(20, v.Z-6)
		if _, next := windup(t, a, []mobs.Body{p}, 1); distance(a.Views()[0], v.X, v.Z) > 1e-9 {
			t.Fatal("the bat moved to spit; want it holding where its spit reaches")
		} else if hits := landed(a, []mobs.Body{p}, next, 24); (len(hits) > 0) == walled { // the windup's 600 ms and 6 m at 9.375 m/s
			t.Fatalf("walled %v: hits %+v", walled, hits)
		}
	}
}
