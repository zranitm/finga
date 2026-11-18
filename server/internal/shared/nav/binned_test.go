package nav_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/internal/shared/sim/simtest"
)

// large-regions.md section 5: a grid built over indexed solids, each cell reading only those within its clearance, is a full scan's to the bit, and so is FitsAt.
func TestAGridOverIndexedSolidsIsAFullScans(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const step = 0.3
		g := simtest.Geometry(t, 0.7)
		ix := sim.Indexed(g)
		scanned, binned := nav.New(g, step), nav.New(ix, step)
		sg, sc, sf := scanned.Cells()
		bg, bc, bf := binned.Cells()
		if !slices.Equal(sg, bg) || !slices.Equal(sc, bc) || !slices.Equal(sf, bf) {
			t.Fatal("the binned grid's cells differ from the scanned grid's")
		}
		for range 50 {
			x, z := simtest.Near(t, g)
			r := rapid.Float64Range(0, nav.MaxClear+1).Draw(t, "r")
			if a, b := nav.FitsAt(g, step, nav.Point{X: x, Z: z}, r), nav.FitsAt(ix, step, nav.Point{X: x, Z: z}, r); a != b {
				t.Fatalf("FitsAt (%v, %v) r %v: %v scanned, %v binned", x, z, r, a, b)
			}
		}
	})
}

// A cell's clearance is exact to a step of 2⁻¹³ m and held at MaxClear: on open ground 40 m square, the cell centred 3.25 m from the west edge takes a radius of 3.25 and not a step more, and the middle cell, 19.75 m from every edge, takes MaxClear and nothing past it.
func TestAClearanceIsExactInStepsAndHeldAtMaxClear(t *testing.T) {
	g := sim.Geometry{SizeX: 40, SizeZ: 40, Terrain: sim.NewTerrain(40, 2, 2, []float64{0, 0, 0, 0}, 0.7)}
	n := nav.New(g, 0.3)
	near, middle := nav.Point{X: 3.25, Z: 20.25}, nav.Point{X: 19.75, Z: 19.75}
	if !n.Fits(near, 3.25) || n.Fits(near, 3.25+1.0/8192) {
		t.Fatal("3.25 m from the edge: want a radius of 3.25 to fit and one step more not to")
	}
	if !n.Fits(middle, nav.MaxClear) || n.Fits(middle, nav.MaxClear+1.0/8192) {
		t.Fatalf("19.75 m from every edge: want MaxClear (%v) to fit and nothing past it", nav.MaxClear)
	}
}
