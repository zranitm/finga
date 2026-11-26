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

// No player walks or jumps onto a cave's rock (underground-and-underwater.md section 4): it's a barrier from a wall's foot, though a steep wall is walked up slantwise. From the start and from each gate, as a cave is entered, the walk still reaches every camp and waypoint.
func TestNoPlayerReachesACavesRock(t *testing.T) {
	data := gamedatatest.Full(t)
	p := shipped(t).params
	caves := 0
	for id, plan := range everyPlan(t) {
		if plan.Rock == nil {
			continue
		}
		caves++
		t.Run(id, func(t *testing.T) {
			_, grid := generated(t, id, plan, p)
			from := map[string][2]float64{"the start": plan.Start.At}
			for _, g := range plan.Gates {
				from["gate "+g.Name] = g.At
			}
			if len(from) < 2 {
				t.Fatal("a cave with no gate")
			}
			for what, at := range from {
				for _, rise := range []float64{0, fastRise(data, p)} {
					got := grid.Reach([]nav.Point{mapfile.Point(at)}, p.Radius, rise)
					for z := 0.25; z < plan.Size[1]; z += 0.5 {
						for x := 0.25; x < plan.Size[0]; x += 0.5 {
							if got.Has(nav.Point{X: x, Z: z}) && solidRock(plan, x, z) {
								t.Fatalf("from %s, a player rising %g reaches (%g, %g), on the rock", what, rise, x, z)
							}
						}
					}
					for _, c := range plan.Camps {
						if !got.Near(mapfile.Point(c.At), c.Radius) {
							t.Errorf("from %s, a player rising %g never reaches camp %s", what, rise, c.Name)
						}
					}
					for _, w := range plan.Waypoints {
						if !got.Near(mapfile.Point(w.At), 2) {
							t.Errorf("from %s, a player rising %g never reaches waypoint %s", what, rise, w.ID)
						}
					}
				}
			}
		})
	}
	if caves == 0 {
		t.Fatal("no plan with rock")
	}
}
