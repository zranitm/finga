package load_test

import (
	"testing"

	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
	"finga/internal/sim"
)

// walker plays a route through a map in movement sub-steps, as a player holding keys would.
type walker struct {
	t *testing.T
	m *gamedata.Map
	p sim.Params
	s sim.State
}

// until holds in until done says so, failing after ten seconds of play.
func (w *walker) until(what string, in sim.Input, done func(sim.State) bool) {
	w.t.Helper()
	for range int(10 / w.p.Dt) {
		if done(w.s) {
			return
		}
		w.s = sim.Step(w.s, in, w.m.Geometry, w.p)
	}
	w.t.Fatalf("%s: stuck at %+v", what, w.s)
}

func standingOn(platform int) func(sim.State) bool {
	return func(s sim.State) bool { return s.OnGround && s.Ground == platform }
}

func platformNamed(t *testing.T, m *gamedata.Map, x, y float64) int {
	t.Helper()
	for i, p := range m.Geometry.Platforms {
		if p.Y == y && x >= p.X && x <= p.X+p.W {
			return i
		}
	}
	t.Fatalf("no platform at (%v, %v)", x, y)
	return -1
}

// The bats fly over the pillar, past the cave's only way in: from the entrance a player walks over the stalagmite, climbs the floor rope to the shelf and the shelf rope above it, and drops onto the pillar.
func TestTheCaveBatsCanBeReachedFromItsEntrance(t *testing.T) {
	data, err := load.Dir("../../../data")
	if err != nil {
		t.Fatal(err)
	}
	cave, _ := data.Map("cave")
	w := &walker{t: t, m: cave, p: data.Movement(), s: sim.State{X: cave.PlayerSpawn.X, Y: cave.PlayerSpawn.Y, OnGround: true, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}}
	shelf, upper, pillar := platformNamed(t, cave, 1000, 416), platformNamed(t, cave, 1100, 288), platformNamed(t, cave, 1500, 384)

	w.until("walk to the stalagmite", sim.Input{Right: true}, func(s sim.State) bool { return s.X >= 680 })
	w.until("jump over it", sim.Input{Right: true, Jump: true}, func(s sim.State) bool { return s.X > 800 && s.OnGround })
	w.until("walk to the floor rope", sim.Input{Right: true}, func(s sim.State) bool { return s.X >= 920 })
	w.until("climb to the shelf", sim.Input{Up: true}, standingOn(shelf))
	w.until("walk to the shelf rope", sim.Input{Right: true}, func(s sim.State) bool { return s.X >= 1184 })
	w.until("climb to the upper shelf", sim.Input{Up: true}, standingOn(upper))
	w.until("walk off onto the pillar", sim.Input{Right: true}, standingOn(pillar))
	for _, sp := range cave.Spawns {
		if sp.Mob == "cave_bat" && !(w.s.X >= sp.Area.X && w.s.X <= sp.Area.X+sp.Area.W) {
			w.t.Fatalf("on the pillar at %+v, outside the bats' area %+v", w.s, sp.Area)
		}
	}
}
