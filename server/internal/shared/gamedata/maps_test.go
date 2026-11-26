package gamedata_test

import (
	"math"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/sim"
)

// A map holds feet on its ground and clear of any solid rising more than a step over them: inside a box a step's height or lower they stand on it; inside a taller one only from within a step of its top.
func TestAMapHoldsFeetClearOfSolidsOverAStep(t *testing.T) {
	const step = 0.3
	m := &gamedata.Map{Geometry: sim.Indexed(sim.Geometry{
		SizeX: 16, SizeZ: 16, Terrain: sim.NewTerrain(16, 2, 2, []float64{0, 0, 0, 0}, 0.7),
		Boxes:     []sim.Box{{X: 4, Z: 4, HalfW: 1, HalfD: 1, UX: 1, Top: 0.25}},
		Cylinders: []sim.Cylinder{{X: 12, Z: 12, Radius: 1, Top: 1}},
	})}
	for _, tt := range []struct {
		name    string
		x, y, z float64
		want    bool
	}{
		{"open ground", 8, 0, 8, true},
		{"in the low box, a step up", 4, 0, 4, true},
		{"in the tall cylinder", 12, 0, 12, false},
		{"in it within a step of its top", 12, 0.8, 12, true},
		{"off the map", -0.1, 0, 8, false},
		{"at no height", 8, math.NaN(), 8, false},
	} {
		if got := m.Holds(tt.x, tt.y, tt.z, step); got != tt.want {
			t.Errorf("%s: holds %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Every camp on the shipped maps and regions refills no sooner than 24 s, so a player has the time to loot a kill's pouch before the next mob comes (loot-pouches.md); test maps keep their own short times.
func TestEveryShippedCampWaitsToRefill(t *testing.T) {
	const least = 24 * time.Second
	camps := 0
	for _, m := range gamedatatest.Full(t).Maps() {
		for _, s := range m.Spawns {
			camps++
			if s.Respawn < least {
				t.Errorf("%s: the %s camp at (%v, %v) refills in %v; want %v or more", m.ID, s.Mob, s.At.X, s.At.Z, s.Respawn, least)
			}
		}
	}
	if camps == 0 {
		t.Fatal("no camps on the shipped maps")
	}
}
