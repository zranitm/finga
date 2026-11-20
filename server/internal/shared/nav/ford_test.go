package nav_test

import (
	"math"
	"os"
	"path/filepath"
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
	fords := 0
	for id, plan := range plans {
		var rivers []*regiongen.River
		for _, f := range plan.Features {
			if f.River != nil && len(f.River.Fords) > 0 {
				rivers = append(rivers, f.River)
			}
		}
		if len(rivers) == 0 {
			continue
		}
		t.Run(id, func(t *testing.T) {
			r, err := regiongen.Generate(plan, shippedKit(t), p.MaxSlope)
			if err != nil {
				t.Fatal(err)
			}
			grid := nav.New(r.Geometry, p.Step)
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
			if nav.FitsAt(g, p.Step, pt, p.Radius) && !g.Terrain.Deep(pt.X, pt.Z) {
				return pt
			}
		}
		t.Fatalf("no spot a body fits beside the ford at %v", at)
		return nav.Point{}
	}
	return side(1), side(-1)
}
