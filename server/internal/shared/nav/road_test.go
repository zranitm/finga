package nav_test

import (
	"math"
	"slices"
	"testing"

	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
)

// painted is a road's drawn curve: its plan path cut at a quarter and three quarters of each segment, three times over (Chaikin's method), its ends kept, as the generator smooths it.
func painted(path [][2]float64) [][2]float64 {
	for range 3 {
		out := [][2]float64{path[0]}
		for i := 0; i+1 < len(path); i++ {
			a, b := path[i], path[i+1]
			out = append(out, [2]float64{a[0] + 0.25*(b[0]-a[0]), a[1] + 0.25*(b[1]-a[1])}, [2]float64{a[0] + 0.75*(b[0]-a[0]), a[1] + 0.75*(b[1]-a[1])})
		}
		path = append(out, path[len(path)-1])
	}
	return path
}

// along is the points of a polyline every step metres of its length, its ends included.
func along(path [][2]float64, step float64) [][2]float64 {
	out := [][2]float64{path[0]}
	walked, next := 0.0, step
	for i := 0; i+1 < len(path); i++ {
		a, b := path[i], path[i+1]
		seg := math.Hypot(b[0]-a[0], b[1]-a[1])
		for next <= walked+seg {
			d := (next - walked) / seg
			out = append(out, [2]float64{a[0] + d*(b[0]-a[0]), a[1] + d*(b[1]-a[1])})
			next += step
		}
		walked += seg
	}
	return append(out, path[len(path)-1])
}

// Every road of every plan can be walked along its painted middle line: no stretch longer than a body's width is steeper than the walkable slope, and a player flooded out from the road's start within the road's own width reaches its end. The ground a road rises to a terrace by is laid back along it for the run it needs.
func TestEveryRoadCanBeWalkedAlongItsMiddle(t *testing.T) {
	p := shipped(t).params
	body := 2 * p.Radius
	roads := 0
	for id, plan := range everyPlan(t) {
		t.Run(id, func(t *testing.T) {
			r, grid := generated(t, id, plan, p)
			for _, f := range plan.Features {
				if f.Road == nil {
					continue
				}
				roads++
				line := along(painted(f.Road.Path), 0.5)
				steep := 0.0 // the length of the steep stretch so far
				for k := 1; k < len(line); k++ {
					a, b := line[k-1], line[k]
					rise := math.Abs(r.Geometry.Terrain.Height(b[0], b[1]) - r.Geometry.Terrain.Height(a[0], a[1]))
					run := math.Hypot(b[0]-a[0], b[1]-a[1])
					if rise/run > p.MaxSlope {
						steep += run
					} else {
						steep = 0
					}
					if steep > body {
						t.Errorf("the road from %v: its middle is steeper than %g for %.1f m by (%.1f, %.1f), %.2f m a metre at the last step, ground %.1f", f.Road.Path[0], p.MaxSlope, steep, b[0], b[1], rise/run, r.Geometry.Terrain.Height(b[0], b[1]))
						break
					}
				}
				// from the first spot of the road a body fits to the last: its very ends may hold a stone, a gate's marker or the water's edge
				at := func(k int) nav.Point { return nav.Point{X: line[k][0], Z: line[k][1]} }
				i0, i1 := 0, len(line)-1
				for !grid.Fits(at(i0), p.Radius) && i0 < i1 {
					i0++
				}
				for !grid.Fits(at(i1), p.Radius) && i1 > i0 {
					i1--
				}
				first, last := at(i0), at(i1)
				got := grid.ReachWithout([]nav.Point{first}, p.Radius, 0, func(c nav.Point) bool { return fromPath(line, c) > f.Road.Width/2 })
				if !got.Has(last) {
					stuck := -1
					for k := i0; k <= i1; k++ {
						if !got.Has(at(k)) {
							stuck = k
							break
						}
					}
					stop := at(stuck)
					t.Errorf("a player from the road's start at %v, kept to its width, never reaches its end at %v: stops at (%.1f, %.1f), ground %.2f", f.Road.Path[0], f.Road.Path[len(f.Road.Path)-1], stop.X, stop.Z, r.Geometry.Terrain.Height(stop.X, stop.Z))
				}
			}
		})
	}
	if roads == 0 {
		t.Fatal("no plan has a road")
	}
	_ = slices.Clone[[]int]
	_ = mapfile.Point
	_ = regiongen.Cliff
}
