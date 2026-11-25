package nav_test

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// Every ford is a way across its river both ways (large-regions.md decision 9), in every shipped region and every test plan: a body walks, as a mob does, with no jump, from the ground past one bank to the ground past the other, staying by the ford.
func TestEveryFordIsCrossedBothWays(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := data.Movement()
	fords := 0
	for id, plan := range everyPlan(t) {
		rivers := fordedRivers(plan)
		if len(rivers) == 0 {
			continue
		}
		t.Run(id, func(t *testing.T) {
			r, grid := generated(t, id, plan, p)
			var f nav.Finder
			for _, river := range rivers {
				for _, at := range river.Fords {
					a, b := banks(t, r.Geometry, p, river, at)
					ford := nav.Point{X: at[0], Z: at[1]}
					leash := math.Hypot(a.X-at[0], a.Z-at[1]) + 4 // round the ford, nowhere near another way across
					for _, way := range [][2]nav.Point{{a, b}, {b, a}} {
						if path := f.Path(grid, way[0], way[1], p.Radius, ford, leash); path == nil {
							t.Errorf("the ford at %v isn't walked across from %v to %v", at, way[0], way[1])
						}
					}
					fords++
				}
			}
		})
	}
	if fords < 4 {
		t.Fatalf("%d fords; the test plans and shipped regions have at least 4", fords)
	}
}

// banks are a spot a body fits on each side of river's ford at: out along the normal to the path's leg nearest the ford, past its banks.
func banks(t *testing.T, g sim.Geometry, p sim.Params, river *regiongen.River, at [2]float64) (nav.Point, nav.Point) {
	t.Helper()
	best, nx, nz := math.Inf(1), 0.0, 0.0
	for i := 1; i < len(river.Path); i++ {
		a, b := river.Path[i-1], river.Path[i]
		dx, dz := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dz)
		s := min(max(((at[0]-a[0])*dx+(at[1]-a[1])*dz)/(l*l), 0), 1)
		if d := math.Hypot(a[0]+s*dx-at[0], a[1]+s*dz-at[1]); d < best {
			best, nx, nz = d, -dz/l, dx/l
		}
	}
	side := func(sign float64) nav.Point {
		for off := river.Width/2 + min(river.Width/4, 2) + 1; off < river.Width+12; off += 0.5 {
			pt := nav.Point{X: at[0] + sign*off*nx, Z: at[1] + sign*off*nz}
			if nav.FitsAt(g, p.Step, pt, p.Radius) && !g.Terrain.Barred(pt.X, pt.Z) {
				return pt
			}
		}
		t.Fatalf("no spot a body fits beside the ford at %v", at)
		return nav.Point{}
	}
	return side(1), side(-1)
}

// everyPlan is every shipped region's plan and every test plan, by name.
func everyPlan(t *testing.T) map[string]regiongen.Plan {
	t.Helper()
	plans := shippedRegions(t)
	for _, glob := range []string{"../regiongen/testdata/*.yaml", "../../../cmd/regionbake/testdata/*.yaml"} {
		files, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := regiongen.Decode(raw)
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			plans[filepath.Base(file)] = plan
		}
	}
	return plans
}

// fordedRivers are plan's rivers with a ford.
func fordedRivers(plan regiongen.Plan) []*regiongen.River {
	var rivers []*regiongen.River
	for _, f := range plan.Features {
		if f.River != nil && len(f.River.Fords) > 0 {
			rivers = append(rivers, f.River)
		}
	}
	return rivers
}

// A river is crossed only at its fords and causeways (from 4b-2b's playtest, where a player walked round both its ends along the ring's cliff): in every shipped region and test plan, a player beside its first ford reaches the other side, walking and jumping; with its crossings taken out, a flood from there reaches one side alone beside each of its fords.
func TestARiverIsCrossedOnlyAtItsFordsAndCauseways(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := data.Movement()
	rise := fastRise(data, p)
	checked := 0
	for id, plan := range everyPlan(t) {
		if len(fordedRivers(plan)) == 0 {
			continue
		}
		t.Run(id, func(t *testing.T) {
			r, grid := generated(t, id, plan, p)
			k := 0
			for _, f := range plan.Features {
				if f.River == nil {
					continue
				}
				river, water := f.River, r.Rivers[k]
				k++
				a, b := banks(t, r.Geometry, p, river, river.Fords[0])
				if !grid.Reach([]nav.Point{a}, p.Radius, rise).Has(b) {
					t.Fatalf("from %v beside the ford at %v, %v across it isn't reached", a, river.Fords[0], b)
				}
				got := grid.ReachWithout([]nav.Point{a}, p.Radius, rise, crossingsOf(plan, river, water))
				for _, at := range river.Fords {
					a, b := banks(t, r.Geometry, p, river, at)
					if got.Has(a) == got.Has(b) {
						t.Errorf("beside the ford at %v, with the river's crossings gone, %v is reached %v and %v across it %v; want one side alone", at, a, got.Has(a), b, got.Has(b))
					}
					checked++
				}
			}
		})
	}
	if checked < 4 {
		t.Fatalf("%d fords checked; the test plans and shipped regions have at least 4", checked)
	}
}

// crossingsOf are river's ways across: its fords' shallows and its causeways, a road's way over it, as a test of a walk grid cell's centre (0.5 m cells) against a mask made once.
func crossingsOf(plan regiongen.Plan, river *regiongen.River, water regiongen.RiverWater) func(nav.Point) bool {
	const cell = 0.5 // the walk grid's
	nx, nz := int(plan.Size[0]/cell), int(plan.Size[1]/cell)
	gone := make([]bool, nx*nz)
	mark := func(x0, z0, x1, z1 float64, in func(nav.Point) bool) {
		for iz := max(int(z0/cell), 0); iz <= min(int(z1/cell), nz-1); iz++ {
			for ix := max(int(x0/cell), 0); ix <= min(int(x1/cell), nx-1); ix++ {
				if in(nav.Point{X: (float64(ix) + 0.5) * cell, Z: (float64(iz) + 0.5) * cell}) {
					gone[iz*nx+ix] = true
				}
			}
		}
	}
	shallows := river.Width/2 + min(river.Width/4, 2) + cell // a ford's, a cell's width over
	for _, at := range river.Fords {
		mark(at[0]-shallows, at[1]-shallows, at[0]+shallows, at[1]+shallows, func(c nav.Point) bool {
			return math.Hypot(c.X-at[0], c.Z-at[1]) <= shallows
		})
	}
	for _, f := range plan.Features {
		if f.Road == nil {
			continue
		}
		reach := f.Road.Width/2 + 3 // past the smoothing of its path
		for i := 1; i < len(f.Road.Path); i++ {
			a, b := f.Road.Path[i-1], f.Road.Path[i]
			leg := [][2]float64{a, b}
			mark(min(a[0], b[0])-reach, min(a[1], b[1])-reach, max(a[0], b[0])+reach, max(a[1], b[1])+reach, func(c nav.Point) bool {
				return fromPath(leg, c) <= reach && fromPath(water.Path, c) <= river.Width/2+1
			})
		}
	}
	return func(c nav.Point) bool { return gone[int(c.Z/cell)*nx+int(c.X/cell)] }
}

var (
	builtMu sync.Mutex
	built   = map[string]struct {
		r    *regiongen.Region
		grid *nav.Grid
	}{}
)

// generated is plan's region and its walk grid, built once a test run however many tests ask.
func generated(t *testing.T, id string, plan regiongen.Plan, p sim.Params) (*regiongen.Region, *nav.Grid) {
	t.Helper()
	builtMu.Lock()
	defer builtMu.Unlock()
	if b, ok := built[id]; ok {
		return b.r, b.grid
	}
	r, err := regiongen.Generate(plan, shippedKit(t), p)
	if err != nil {
		t.Fatal(err)
	}
	grid := nav.New(r.Geometry, p.Step)
	built[id] = struct {
		r    *regiongen.Region
		grid *nav.Grid
	}{r, grid}
	return r, grid
}

// fromPath is how far at lies from path.
func fromPath(path [][2]float64, at nav.Point) float64 {
	best := math.Inf(1)
	for i := 1; i < len(path); i++ {
		a, b := path[i-1], path[i]
		dx, dz := b[0]-a[0], b[1]-a[1]
		s := min(max(((at.X-a[0])*dx+(at.Z-a[1])*dz)/(dx*dx+dz*dz), 0), 1)
		best = min(best, math.Hypot(a[0]+s*dx-at.X, a[1]+s*dz-at.Z))
	}
	return best
}
