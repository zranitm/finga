package mobs

import (
	"math/rand/v2"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// meadow is an arena on a flat 40 m meadow with boxes and spawns.
func meadow(t *testing.T, boxes []sim.Box, spawns ...gamedata.Spawn) *Arena {
	t.Helper()
	data := gamedatatest.Shipped(t)
	g := sim.Geometry{SizeX: 40, SizeZ: 40, Terrain: sim.NewTerrain(40, 2, 2, []float64{0, 0, 0, 0}, 0.7), Cylinders: []sim.Cylinder{}, Boxes: boxes}
	grid := nav.New(g, data.Movement().Step)
	m := &gamedata.Map{ID: "meadow", Kind: gamedata.Hunting, Geometry: g, Spawns: spawns, Grid: func() *nav.Grid { return grid }}
	return NewArena(Deps{Map: m, Data: data, Roll: rand.New(rand.NewPCG(1, 3))}, Tuning{TickInterval: 50 * time.Millisecond})
}

// Two slimes wandering head-on along one of the grid's lines, each to a spot past the other, pass each other before either gets nowhere for stuckFor: spread held two walkers crowdedBy deep in each other, each pushed back as far as it walked, and in the 4b-1 playtest such a pair stood facing each other for minutes.
func TestTwoWanderersMeetingHeadOnPassEachOther(t *testing.T) {
	for _, paces := range [][2]float64{{1, 1}, {1.1, 0.9}} {
		a := meadow(t, nil, gamedata.Spawn{Mob: "green_slime", Max: 2, Respawn: 5 * time.Second, At: gamedata.Point{X: 20, Z: 20}, Radius: 4})
		south, north := a.mobs[0], a.mobs[1]
		south.x, south.z, south.pace, south.path = 20.25, 18, paces[0], []nav.Point{{X: 20.25, Z: 23}}
		north.x, north.z, north.pace, north.path = 20.25, 22, paces[1], []nav.Point{{X: 20.25, Z: 17}}
		for _, mb := range []*mob{south, north} {
			mb.pauseUntil = 0
			a.bodies.moved(mb)
		}
		most := a.ticks(stuckFor)
		tick := uint64(1)
		for ; south.z <= north.z && tick <= most; tick++ {
			a.Step(tick, nil, nil)
		}
		if south.z <= north.z {
			t.Errorf("paces %v: after %d ticks the south slime is at (%.3f, %.3f), the north one at (%.3f, %.3f); want them past each other", paces, most, south.x, south.z, north.x, north.z)
			continue
		}
		t.Logf("paces %v: past each other on tick %d", paces, tick)
	}
}

// pushedBack steps the arena ticks from 1 to n with mb put back where it stood after each, as spread put back two walkers met on one line: every step its walk made, it was pushed back as far.
func pushedBack(a *Arena, mb *mob, n uint64, each func(tick uint64)) {
	x, z := mb.x, mb.z
	for tick := uint64(1); tick <= n; tick++ {
		a.Step(tick, nil, nil)
		mb.x, mb.z = x, z
		a.bodies.moved(mb)
		each(tick)
	}
}

// A slime wandering to a spot gives up its walk once it has got nowhere for stuckFor, though each step it made went through, rather than stand there until a player comes by.
func TestAWandererGettingNowhereGivesUpItsWalk(t *testing.T) {
	a := meadow(t, nil, gamedata.Spawn{Mob: "green_slime", Max: 1, Respawn: 5 * time.Second, At: gamedata.Point{X: 20, Z: 20}, Radius: 4})
	mb := a.mobs[0]
	end := nav.Point{X: 20, Z: 23}
	mb.x, mb.z, mb.path, mb.pauseUntil = 20, 18, []nav.Point{end}, 0
	mb.setOff(0)
	a.bodies.moved(mb)
	gaveUp := uint64(0)
	pushedBack(a, mb, a.ticks(stuckFor)+2, func(tick uint64) {
		if gaveUp == 0 && (len(mb.path) == 0 || mb.path[len(mb.path)-1] != end) {
			gaveUp = tick
		}
	})
	if want := a.ticks(stuckFor) + 1; gaveUp != want {
		t.Fatalf("the slime gave up its walk on tick %d (0 for never); want tick %d, once stuckFor had passed", gaveUp, want)
	}
}

// A mob walking home stops once it has got nowhere for stuckFor, though each step it made went through: it counted a step as getting somewhere, so one pushed back as far as it walked went home for good.
func TestAMobGoingHomeGettingNowhereStops(t *testing.T) {
	a := meadow(t, nil, gamedata.Spawn{Mob: "green_slime", Max: 1, Respawn: 5 * time.Second, At: gamedata.Point{X: 20, Z: 30}, Radius: 1})
	mb := a.mobs[0]
	mb.x, mb.z = 20, 18
	a.bodies.moved(mb)
	a.giveUp(mb)
	stopped := uint64(0)
	pushedBack(a, mb, a.ticks(stuckFor)+2, func(tick uint64) {
		if stopped == 0 && !mb.homing {
			stopped = tick
		}
	})
	if want := a.ticks(stuckFor) + 1; stopped != want {
		t.Fatalf("the slime stopped going home on tick %d (0 for never); want tick %d, once stuckFor had passed", stopped, want)
	}
}

// A slime that paused longer than stuckFor walks its next wander as any other: getting nowhere counts from where each walk sets off.
func TestAWandererAfterALongPauseWalksOn(t *testing.T) {
	a := meadow(t, nil, gamedata.Spawn{Mob: "green_slime", Max: 1, Respawn: 5 * time.Second, At: gamedata.Point{X: 20, Z: 20}, Radius: 4})
	mb := a.mobs[0]
	mb.path, mb.pauseUntil = nil, 2*a.ticks(stuckFor)
	mb.setOff(0)
	tick := uint64(1)
	for ; tick <= mb.pauseUntil; tick++ {
		a.Step(tick, nil, nil)
	}
	from := mb.at()
	for end := tick + a.ticks(time.Second); tick < end; tick++ {
		a.Step(tick, nil, nil)
	}
	if went := nav.Distance(mb.at(), from); went < progressStep {
		t.Fatalf("the slime went %.3f m in the second after its pause; want at least %v, walking", went, progressStep)
	}
}
