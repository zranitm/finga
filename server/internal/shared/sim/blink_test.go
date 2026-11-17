package sim_test

import (
	"math"
	"testing"

	"finga/internal/shared/sim"
)

// A blink never ends inside a mob's body, whatever its length, past one mob, between two, or against a large one beside its way.
func TestABlinkNeverEndsInAMob(t *testing.T) {
	for _, mobs := range [][]sim.Blocker{
		mob,
		{{X: 7, Z: 6, Radius: 0.5, Top: 1}, {X: 8.2, Z: 6, Radius: 0.6, Top: 1}},
		{{X: 8, Z: 6.9, Radius: 1.5, Top: 3}},
	} {
		g := flat()
		g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
		g.Blockers = mobs
		for i := range 161 {
			length := float64(i) * 0.05
			end := sim.Blink(standing(4, 0, 6), sim.Way{X: 1, Length: length}, g, params)
			for _, b := range mobs {
				if math.Hypot(end.X-b.X, end.Z-b.Z) < b.Radius+params.Radius {
					t.Fatalf("a %v m blink past %+v ended at %.3f, inside %+v", length, mobs, end.X, b)
				}
			}
			if end.X < 4 || end.X > 4+length+1e-9 { // the steps' sum may round past the length
				t.Fatalf("a %v m blink ended at %.3f, outside its way", length, end.X)
			}
		}
	}
}
