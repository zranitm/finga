package nav_test

import (
	"slices"
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
)

// No player walks or jumps up a cliff plateau's face (large-regions.md section 3): it's a barrier, though a steep face is walked up slantwise. Only a road onto the plateau reaches its top, and it does.
func TestNoPlayerClimbsACliff(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	walled, roaded := 0, 0
	for id, plan := range everyPlan(t) {
		var cliffs []regiongen.Plateau
		for _, f := range plan.Features {
			if f.Plateau != nil && f.Plateau.Edge == regiongen.Cliff && f.Plateau.Height > 0 {
				cliffs = append(cliffs, *f.Plateau)
			}
		}
		if len(cliffs) == 0 {
			continue
		}
		t.Run(id, func(t *testing.T) {
			_, grid := generated(t, id, plan, p)
			for _, rise := range []float64{0, fastRise(data, p)} {
				got := grid.Reach([]nav.Point{mapfile.Point(plan.Start.At)}, p.Radius, rise)
				for _, c := range cliffs {
					road := slices.ContainsFunc(plan.Features, func(f regiongen.Feature) bool {
						return f.Road != nil && slices.ContainsFunc(f.Road.Path, func(at [2]float64) bool { return inside(c.Area, at) })
					})
					edge := append(slices.Clone(c.Area), c.Area[0])
					reached := [2]float64{-1, -1}
					for z := 0.25; z < plan.Size[1] && reached[0] < 0; z += 0.5 {
						for x := 0.25; x < plan.Size[0]; x += 0.5 {
							if at := (nav.Point{X: x, Z: z}); inside(c.Area, [2]float64{x, z}) && fromPath(edge, at) > 2 && got.Has(at) {
								reached = [2]float64{x, z}
								break
							}
						}
					}
					switch {
					case road && reached[0] < 0:
						t.Errorf("a player rising %g never reaches the top of the cliff plateau at %v by its road", rise, c.Area[0])
					case !road && reached[0] >= 0:
						t.Errorf("a player rising %g reaches %v, on the cliff plateau at %v with no road onto it", rise, reached, c.Area[0])
					}
					if rise == 0 && road {
						roaded++
					} else if rise == 0 {
						walled++
					}
				}
			}
		})
	}
	if walled == 0 || roaded == 0 {
		t.Fatalf("%d cliff plateaus with no road, %d with one; want both", walled, roaded)
	}
}
