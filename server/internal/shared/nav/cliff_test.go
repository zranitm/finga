package nav_test

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// No player walks or jumps up a cliff plateau's face (large-regions.md section 3): it's a barrier, though a steep face is walked up slantwise. Only a road onto the plateau reaches its top, and it does.
func TestNoPlayerClimbsACliff(t *testing.T) {
	data := gamedatatest.Full(t)
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
					reached, hit := topReached(c, got)
					switch {
					case road && !hit:
						t.Errorf("a player rising %g never reaches the top of the cliff plateau at %v by its road", rise, c.Area[0])
					case !road && hit:
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

// topReached is the first cell, by rows from the south, on the top of cliff plateau c, a metre or more in from its face, that got holds; false when none.
func topReached(c regiongen.Plateau, got nav.Reached) ([2]float64, bool) {
	edge := append(slices.Clone(c.Area), c.Area[0])
	lo, hi := bounds(c.Area)
	for z := lo[1] + 0.25; z < hi[1]; z += 0.5 {
		for x := lo[0] + 0.25; x < hi[0]; x += 0.5 {
			if at := (nav.Point{X: x, Z: z}); inside(c.Area, [2]float64{x, z}) && fromPath(edge, at) > 2 && got.Has(at) {
				return [2]float64{x, z}, true
			}
		}
	}
	return [2]float64{-1, -1}, false
}

// bounds is the box round poly's corners, its least x and z and its most.
func bounds(poly [][2]float64) (lo, hi [2]float64) {
	lo, hi = poly[0], poly[0]
	for _, p := range poly[1:] {
		lo = [2]float64{min(lo[0], p[0]), min(lo[1], p[1])}
		hi = [2]float64{max(hi[0], p[0]), max(hi[1], p[1])}
	}
	return lo, hi
}

// A road eases a cliff plateau's face: roadMean in regiongen, how far along it its height is the ground's mean, and the plateau's top near the face is a ramp.
const rampLength = 15 + 5 // roadMean, and a margin for a road that bends

// rampsOnto is a test of a walk grid cell's centre against the ramps of the plan's roads onto cliff plateau c, a mask made once: every cell of such a road's footprint within rampLength of the plateau's face. A road is onto the plateau when a metre along its path lies inside it.
func rampsOnto(plan regiongen.Plan, c regiongen.Plateau) func(nav.Point) bool {
	const cell = 0.5 // the walk grid's
	nx, nz := int(plan.Size[0]/cell), int(plan.Size[1]/cell)
	gone := make([]bool, nx*nz)
	edge := append(slices.Clone(c.Area), c.Area[0])
	for _, f := range plan.Features {
		if f.Road == nil || !roadOnto(f.Road, c) {
			continue
		}
		reach := f.Road.Width*1.5 + 1 // the road and its shoulder, a cell over
		for i := 1; i < len(f.Road.Path); i++ {
			a, b := f.Road.Path[i-1], f.Road.Path[i]
			leg := [][2]float64{a, b}
			for iz := max(int((min(a[1], b[1])-reach)/cell), 0); iz <= min(int((max(a[1], b[1])+reach)/cell), nz-1); iz++ {
				for ix := max(int((min(a[0], b[0])-reach)/cell), 0); ix <= min(int((max(a[0], b[0])+reach)/cell), nx-1); ix++ {
					at := nav.Point{X: (float64(ix) + 0.5) * cell, Z: (float64(iz) + 0.5) * cell}
					if fromPath(leg, at) <= reach && fromPath(edge, at) <= rampLength {
						gone[iz*nx+ix] = true
					}
				}
			}
		}
	}
	return func(p nav.Point) bool {
		ix, iz := int(p.X/cell), int(p.Z/cell)
		return ix >= 0 && iz >= 0 && ix < nx && iz < nz && gone[iz*nx+ix]
	}
}

// roadOnto reports whether road runs inside plateau c somewhere, a metre at a time.
func roadOnto(road *regiongen.Road, c regiongen.Plateau) bool {
	for i := 1; i < len(road.Path); i++ {
		a, b := road.Path[i-1], road.Path[i]
		n := int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1])))
		for k := 0; k <= n; k++ {
			s := float64(k) / float64(max(n, 1))
			if inside(c.Area, [2]float64{a[0] + s*(b[0]-a[0]), a[1] + s*(b[1]-a[1])}) {
				return true
			}
		}
	}
	return false
}

// plateauStart is a place a player comes in at: the plan's start or a gate.
type plateauStart struct {
	what string
	at   [2]float64
}

// plateauLeak is the first cell of cliff plateau c's top that a player reaches from the plan's start or any gate, with every ramp of its roads gone, rising rise at most, and how: which place they came in at, walking or at the most a jump lifts. A start on the plateau itself is where its players arrive, so it floods nothing.
func plateauLeak(grid *nav.Grid, plan regiongen.Plan, c regiongen.Plateau, p sim.Params, rise float64) (at [2]float64, how string, found bool) {
	gone := rampsOnto(plan, c)
	var starts []plateauStart
	for _, s := range append([]plateauStart{{"the start", plan.Start.At}}, gatesOf(plan)...) {
		if !inside(c.Area, s.at) {
			starts = append(starts, s)
		}
	}
	all := make([]nav.Point, len(starts))
	for i, s := range starts {
		all[i] = mapfile.Point(s.at)
	}
	if at, found = topReached(c, grid.ReachWithout(all, p.Radius, rise, gone)); !found {
		return at, "", false
	}
	for _, r := range []float64{0, rise} { // one flood each, only when there is a leak to explain
		for i, s := range starts {
			if _, hit := topReached(c, grid.ReachWithout([]nav.Point{all[i]}, p.Radius, r, gone)); hit {
				if r == 0 {
					return at, "walking from " + s.what, true
				}
				return at, fmt.Sprintf("rising %.2g from %s", r, s.what), true
			}
		}
	}
	return at, "", true
}

// gatesOf are plan's gates as places a player comes in at.
func gatesOf(plan regiongen.Plan) []plateauStart {
	out := make([]plateauStart, len(plan.Gates))
	for i, g := range plan.Gates {
		out[i] = plateauStart{"gate " + g.Name, g.At}
	}
	return out
}

// The only way onto a cliff plateau is a road (large-regions.md section 3): in every shipped region and test plan, with the ramps of the roads onto a plateau taken out, a player from the start or any gate, walking and at the fastest jump, reaches no cell of its top. TestNoPlayerClimbsACliff asks a plateau with a road is reached by it and one without isn't; this asks whether a plateau with a road can also be reached some other way.
func TestAPlateauIsReachedOnlyByItsRoads(t *testing.T) {
	data := gamedatatest.Full(t)
	p := shipped(t).params
	rise := fastRise(data, p)
	checked := 0
	for id, plan := range everyPlan(t) {
		var cliffs []regiongen.Plateau
		for _, f := range plan.Features {
			if f.Plateau != nil && f.Plateau.Edge == regiongen.Cliff && f.Plateau.Height > 0 && slices.ContainsFunc(plan.Features, func(g regiongen.Feature) bool { return g.Road != nil && roadOnto(g.Road, *f.Plateau) }) {
				cliffs = append(cliffs, *f.Plateau)
			}
		}
		if len(cliffs) == 0 {
			continue
		}
		t.Run(id, func(t *testing.T) {
			_, grid := generated(t, id, plan, p)
			for _, c := range cliffs {
				began := time.Now()
				at, how, found := plateauLeak(grid, plan, c, p, rise)
				t.Logf("the plateau at %v: %v", c.Area[0], time.Since(began).Round(time.Millisecond))
				if found {
					t.Errorf("%s: with its roads' ramps gone, a player reaches %v on the cliff plateau at %v, %s", id, at, c.Area[0], how)
				}
				checked++
			}
		})
	}
	if checked == 0 {
		t.Fatal("no cliff plateau with a road onto it, so nothing checked")
	}
}

// A plateau with a gap in its cliff is found (the plant that must fail): a stair of boxes against its west face, each a jump higher than the one before, leaves a way up that is no road, which a player jumps with a road's ramp gone; with no stair, none reaches the top.
func TestAPlateauWithAGapInItsCliffIsFound(t *testing.T) {
	plateau := regiongen.Plateau{Area: [][2]float64{{40, 40}, {70, 40}, {70, 70}, {40, 70}}, Height: 6, Edge: regiongen.Cliff}
	plan := flatRegion()
	plan.Features = []regiongen.Feature{{Plateau: &plateau}}
	plan.Start = regiongen.Place{At: [2]float64{20, 55}}
	cliff := sim.Box{X: 55, Z: 55, HalfW: 15, HalfD: 15, UX: 1, Top: 6}
	stair := []sim.Box{
		{X: 31.5, Z: 55, HalfW: 1.5, HalfD: 5, UX: 1, Top: 1.5},
		{X: 34.5, Z: 55, HalfW: 1.5, HalfD: 5, UX: 1, Top: 3},
		{X: 38, Z: 55, HalfW: 2, HalfD: 5, UX: 1, Top: 4.5},
	}
	for _, c := range []struct {
		name  string
		boxes []sim.Box
		leaks bool
	}{
		{"no gap", []sim.Box{cliff}, false},
		{"a stair", append([]sim.Box{cliff}, stair...), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			grid := nav.New(flat(128, 128, c.boxes, nil), params.Step)
			at, how, found := plateauLeak(grid, plan, plateau, params, rise)
			if found != c.leaks {
				t.Fatalf("with %s, the plateau's top is reached: %v (%v, %s); want %v", c.name, found, at, how, c.leaks)
			}
			if found {
				t.Logf("found at %v, %s", at, how)
			}
		})
	}
}
