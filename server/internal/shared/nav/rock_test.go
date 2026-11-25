package nav_test

import (
	"slices"
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
)

// solidRock is whether (x, z) lies deep in plan's rock: inside its ring's face, and well past every hall's and tunnel's wall, however its line wanders.
func solidRock(plan regiongen.Plan, x, z float64) bool {
	if ringDepth(plan, x, z) > 0 {
		return false
	}
	past := plan.Rock.Rough + float64(2*plan.Cell) + 2
	at := [2]float64{x, z}
	for _, f := range plan.Features {
		switch {
		case f.Hall != nil:
			edge := append(slices.Clone(f.Hall.Area), f.Hall.Area[0])
			if inside(f.Hall.Area, at) || fromPath(edge, nav.Point{X: x, Z: z}) < past {
				return false
			}
		case f.Tunnel != nil:
			if fromPath(f.Tunnel.Path, nav.Point{X: x, Z: z}) < float64(f.Tunnel.Width/2)+past {
				return false
			}
		}
	}
	return true
}

// inside is whether at lies in poly.
func inside(poly [][2]float64, at [2]float64) bool {
	in := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		if (a[1] <= at[1]) != (b[1] <= at[1]) && a[0]+float64((at[1]-a[1])/(b[1]-a[1])*(b[0]-a[0])) < at[0] {
			in = !in
		}
	}
	return in
}

// No player walks or jumps onto a cave's rock (underground-and-underwater.md section 4): a wall's foot marks it as deep water is, though a steep wall is walked up slantwise. The walk still reaches every camp and waypoint.
func TestNoPlayerReachesACavesRock(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	caves := 0
	for id, plan := range everyPlan(t) {
		if plan.Rock == nil {
			continue
		}
		caves++
		t.Run(id, func(t *testing.T) {
			_, grid := generated(t, id, plan, p)
			for _, rise := range []float64{0, fastRise(data, p)} {
				got := grid.Reach([]nav.Point{mapfile.Point(plan.Start.At)}, p.Radius, rise)
				for z := 0.25; z < plan.Size[1]; z += 0.5 {
					for x := 0.25; x < plan.Size[0]; x += 0.5 {
						if got.Has(nav.Point{X: x, Z: z}) && solidRock(plan, x, z) {
							t.Fatalf("a player rising %g reaches (%g, %g), on the rock", rise, x, z)
						}
					}
				}
				for _, c := range plan.Camps {
					if !got.Near(mapfile.Point(c.At), c.Radius) {
						t.Errorf("a player rising %g never reaches camp %s", rise, c.Name)
					}
				}
				for _, w := range plan.Waypoints {
					if !got.Near(mapfile.Point(w.At), 2) {
						t.Errorf("a player rising %g never reaches waypoint %s", rise, w.ID)
					}
				}
			}
		})
	}
	if caves == 0 {
		t.Fatal("no plan with rock")
	}
}
