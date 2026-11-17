package sim_test

import (
	"math"
	"testing"

	"finga/internal/shared/sim"
)

// flatWith is flat ground with the given solids, its terrain built as a map's is.
func flatWith(cylinders ...sim.Cylinder) sim.Geometry {
	g := flat()
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
	g.Cylinders = cylinders
	return g
}

// A body pressing into a trunk off its middle slides round it along the tangent and walks on, never inside it; pressed straight at its middle, it stays at the rim.
func TestABodyPressingPastATrunkGoesRoundIt(t *testing.T) {
	trunk := sim.Cylinder{X: 8, Z: 6, Radius: 1, Top: 7}
	g := flatWith(trunk)
	reach := trunk.Radius + params.Radius
	for _, tt := range []struct {
		name   string
		start  sim.State
		in     sim.Input
		past   bool // ends beyond the trunk the way it was pressed, else stays at its rim
		ux, uz float64
	}{
		{"north-east, meeting it near its middle", standing(5, 0, 3.4), northEast, true, sim.Diagonal, sim.Diagonal},
		{"east, 0.3 m north of its middle", standing(5, 0, 6.3), east, true, 1, 0},
		{"east, at its middle", standing(5, 0, 6), east, false, 1, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.start
			for range 150 {
				s = sim.Step(s, tt.in, g, params)
				if d := math.Hypot(s.X-trunk.X, s.Z-trunk.Z); d < reach-1e-9 {
					t.Fatalf("at (%v, %v), %v m from the trunk's centre: inside it", s.X, s.Z, d)
				}
			}
			beyond := (s.X-trunk.X)*tt.ux + (s.Z-trunk.Z)*tt.uz
			if tt.past && beyond <= reach {
				t.Fatalf("ends at (%v, %v), %v m past the trunk's centre; want round it, over %v", s.X, s.Z, beyond, reach)
			}
			if !tt.past && (math.Abs(s.X-(trunk.X-reach)) > 1e-9 || s.Z != tt.start.Z) {
				t.Fatalf("ends at (%v, %v); want stopped at its rim, (%v, %v)", s.X, s.Z, trunk.X-reach, tt.start.Z)
			}
		})
	}
}

// Only a step slides round a trunk: a push, which carries charges, spat balls and knockback, still stops at its rim.
func TestAPushStillStopsAtATrunk(t *testing.T) {
	g := flatWith(sim.Cylinder{X: 8, Z: 6.3, Radius: 1, Top: 7})
	s := sim.Push(standing(5, 0, 6), 4, 0, g, params)
	want := 8 - math.Sqrt(1.375*1.375-0.3*0.3)
	if math.Abs(s.X-want) > 1e-9 || s.Z != 6 {
		t.Fatalf("pushed to (%v, %v); want stopped at the rim, (%v, 6)", s.X, s.Z, want)
	}
}
