package mobs_test

import (
	"math"
	"testing"
	"time"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// weak is a blow that only wakes what it reaches.
var weak = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1}

// standing is the king at its spot, 3 m round (30, 30), on an open meadow, and a clock of ticks: run steps the arena n ticks with bodies and reports whether it bit id.
func standing(t *testing.T) (*mobs.Arena, func(n int, id mobs.Body, bodies ...mobs.Body) bool) {
	t.Helper()
	a := newArena(t, meadow(t, open()), worldtest.FixedRoll{V: 0.5})
	def, spot := king(t)
	a.SpawnBoss(def, spot)
	var tick uint64
	return a, func(n int, id mobs.Body, bodies ...mobs.Body) bool {
		bit := false
		for range n {
			tick++
			a.Step(tick, bodies)
			bit = bit || bitten(a, id.ID)
		}
		return bit
	}
}

// wake is p's weak area blow on the boss.
func wake(p mobs.Body) mobs.Strike {
	return mobs.Strike{By: p.ID, Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 30}, Most: 1, Power: weak, Aim: model.Aim{X: p.X, Z: p.Z}}
}

// A standing boss sees nobody until someone hits it: a player beside it is let be; one blow and it fights them; provoked from past its leash it walks home and is calm again; and once nobody is left to fight it calms too.
func TestAStandingBossFightsOnlyOnceHit(t *testing.T) {
	a, run := standing(t)
	p := player(30, 27) // beside its circle, in its sight and its reach
	if run(200, p, p) {
		t.Fatal("a boss nobody hit bit a player beside it")
	}
	a.Strike(wake(p))
	if !run(200, p, p) {
		t.Fatal("the boss never fought the player who hit it")
	}
	far := p
	far.Z = 5 // 25 m south of its spot: past its 19 m leash
	a.Strike(wake(far))
	run(600, p, far) // provoked from past its leash, it gives up and walks home
	if run(200, p, p) {
		t.Fatal("the boss, provoked from past its leash and home again, bit a player beside it")
	}
	a.Strike(wake(p))
	if !run(200, p, p) {
		t.Fatal("woken again, the boss never fought")
	}
	run(200, p) // the player gone: nobody left to fight
	if run(200, p, p) {
		t.Fatal("the boss, with nobody left, stayed awake and bit a player coming back")
	}
}

// A standing boss still awake when its map empties gives up as it would with nobody left to fight: on the empty map it walks home and calms, so whoever comes next, even beside its spot, is let be.
func TestABossAwakeWhenItsMapEmptiesWalksHomeAndCalms(t *testing.T) {
	for _, tt := range []struct {
		name string
		by   mobs.Body // who woke it, where the boss fought them as the map emptied
	}{
		{"awake at its spot", player(30, 27)},
		{"chasing, out of its circle", player(30, 20)}, // 10 m south, within its leash
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newArena(t, meadow(t, open()), worldtest.FixedRoll{V: 0.5})
			def, spot := king(t)
			a.SpawnBoss(def, spot)
			var n uint64
			step := func(ticks int, bodies ...mobs.Body) (bit bool) { // with nobody given, the map is empty and rests, as an instance's does
				for range ticks {
					n++
					if len(bodies) == 0 {
						a.Rest(n)
						continue
					}
					a.Step(n, bodies)
					bit = bit || bitten(a, bodies[0].ID)
				}
				return bit
			}
			fromSpot := func() float64 {
				v := a.Views()[0]
				return math.Hypot(v.X-spot.At.X, v.Z-spot.At.Z)
			}
			a.Strike(wake(tt.by))
			if !step(200, tt.by) {
				t.Fatal("the boss never fought the player who woke it")
			}
			if out := fromSpot() > spot.Radius; out != (tt.by.Z < 25) {
				t.Fatalf("the boss %.1f m from its spot as the map empties", fromSpot())
			}
			step(int(20 * time.Second / tick))
			if d := fromSpot(); d > spot.Radius {
				t.Fatalf("the boss %.1f m from its spot after 20 s of an empty map, want home", d)
			}
			if stranger := player(30, 27); step(200, stranger) {
				t.Fatal("the boss bit a stranger who came after the map emptied, beside its spot")
			}
		})
	}
}

// Hiding from a calm boss doesn't wake it, but a blow does, even from someone it can't see; awake, it comes after whoever it sees.
func TestABlowFromSomeoneHiddenWakesAStandingBoss(t *testing.T) {
	a, run := standing(t)
	hider, other := player(30, 27), player(27, 30)
	a.Afflict(inflicting(t, mobs.Strike{By: hider.ID, Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 30}, Most: 1, Aim: model.Aim{X: 30, Z: 27}}, gamedata.LoseSight, 30*time.Second, 0))
	if run(200, other, hider, other) {
		t.Fatal("a smoke on a calm boss woke it")
	}
	a.Strike(wake(hider))
	if !run(200, other, hider, other) {
		t.Fatal("a blow from someone the boss can't see left it calm")
	}
}

// A status skill that hides its user and does more wakes a standing boss: only one that does nothing but hide leaves it calm.
func TestAHidingSkillThatAlsoSlowsWakesAStandingBoss(t *testing.T) {
	a, run := standing(t)
	hider, other := player(30, 27), player(27, 30)
	s := mobs.Strike{By: hider.ID, Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 30}, Most: 1, Aim: model.Aim{X: 30, Z: 27}}
	a.Afflict(inflicting(t, inflicting(t, s, gamedata.LoseSight, 30*time.Second, 0), "slow", 5*time.Second, 0))
	if !run(200, other, hider, other) {
		t.Fatal("a smoke that also slows left the boss calm")
	}
}

// A standing boss's circle leaves its neighbours as they are: a slime in it still comes after a player it sees.
func TestMobsByAStandingBossStillFight(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 1, 30, 30, 1)), worldtest.FixedRoll{V: 0.5})
	def, spot := king(t)
	a.SpawnBoss(def, spot)
	p := player(30, 27)
	bit := false
	for n := range uint64(200) {
		a.Step(n+1, []mobs.Body{p})
		bit = bit || bitten(a, p.ID)
	}
	if !bit {
		t.Fatal("the slime by the boss never fought")
	}
}
