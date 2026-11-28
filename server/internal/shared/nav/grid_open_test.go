package nav

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"finga/internal/shared/sim"
)

// The grid reads no solid for a cell none is within reach of, and gets what it reads for the rest: every cell of a grid over scattered solids, near the edge and far from them, holds what stand finds for it by the full search.
func TestTheGridsCellsAreWhatTheFullSearchFinds(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 7))
	const size = 120.0
	nx := int(size/2) + 1
	heights := make([]float64, nx*nx)
	for i := range heights {
		heights[i] = rng.Float64() * 3
	}
	g := sim.Geometry{SizeX: size, SizeZ: size, Terrain: sim.NewTerrain(2, nx, nx, heights, 1.2)}
	for range 40 { // a clump in one corner, and the rest of the map bare
		g.Cylinders = append(g.Cylinders, sim.Cylinder{X: 5 + rng.Float64()*30, Z: 5 + rng.Float64()*30, Radius: 0.3 + rng.Float64()*2, Top: 1 + rng.Float64()*8})
	}
	for range 6 {
		g.Boxes = append(g.Boxes, sim.Box{X: 40 + rng.Float64()*20, Z: 80 + rng.Float64()*30, HalfW: 1 + rng.Float64()*3, HalfD: 1 + rng.Float64()*3, UX: 1, Top: 1 + rng.Float64()*8})
	}
	for _, indexed := range []bool{true, false} {
		geo := g
		if indexed {
			geo = sim.Indexed(g)
		}
		grid := New(geo, 0.5)
		open, searched := 0, 0
		mask := solidReach(geo)
		for i := range grid.nx * grid.nz {
			c := grid.centre(i)
			want := stand(geo, 0.5, c)
			if mask.near(c) {
				searched++
			} else {
				open++
			}
			if got := grid.clear[i]; got != uint16(want.clear/clearUnit) {
				t.Fatalf("indexed %v: cell %d at %v: clearance %d, want %d from the full search", indexed, i, c, got, uint16(want.clear/clearUnit))
			}
			if gotRaised, gotBarred := grid.flags[i]&raised != 0, grid.flags[i]&barred != 0; gotRaised != want.raised || gotBarred != want.barred {
				t.Fatalf("indexed %v: cell %d at %v: raised %v barred %v, want %v %v", indexed, i, c, gotRaised, gotBarred, want.raised, want.barred)
			}
		}
		if open < searched {
			t.Fatalf("indexed %v: %d cells open and %d searched; the test's bare ground should be most of the map", indexed, open, searched)
		}
	}
}

// fullSearch is New as it was: every cell found by stand, whatever is near.
func fullSearch(g sim.Geometry, step float64) *Grid {
	n := &Grid{nx: int(math.Ceil(g.SizeX / cell)), nz: int(math.Ceil(g.SizeZ / cell)), g: g, step: step}
	size := n.nx * n.nz
	n.clear, n.flags = make([]uint16, size), make([]uint8, size)
	ground, marks := make([]float64, size), make([]uint8, size)
	for i := range size {
		c := n.centre(i)
		s := stand(g, step, c)
		ground[i], n.clear[i] = s.ground, uint16(math.Floor(s.clear/clearUnit))
		n.flags[i] = flag(s.raised, raised) | flag(s.barred, barred)
		marks[i] = flag(s.onTop, onTop) | flag(g.Terrain.Steep(c.X, c.Z, false), steepX) | flag(g.Terrain.Steep(c.X, c.Z, true), steepZ)
	}
	for i := range size {
		for k, j := range n.sides(i) {
			if j >= 0 && n.open(ground, marks, i, j) {
				n.flags[i] |= toWest << k
			}
		}
	}
	return n
}

// The whole grid, every clearance and every flag with the ways open, is what the full search builds.
func TestTheGridIsTheFullSearchsWhole(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 3))
	const size = 90.0
	nx := int(size/2) + 1
	heights := make([]float64, nx*nx)
	for i := range heights {
		heights[i] = rng.Float64() * 4
	}
	g := sim.Geometry{SizeX: size, SizeZ: size, Terrain: sim.NewTerrain(2, nx, nx, heights, 1.2)}
	for range 25 {
		g.Cylinders = append(g.Cylinders, sim.Cylinder{X: rng.Float64() * size, Z: rng.Float64() * 30, Radius: 0.3 + rng.Float64()*2, Top: rng.Float64() * 9})
	}
	for range 4 {
		g.Boxes = append(g.Boxes, sim.Box{X: rng.Float64() * size, Z: 50 + rng.Float64()*40, HalfW: 1 + rng.Float64()*3, HalfD: 1 + rng.Float64()*3, UX: 1, Top: rng.Float64() * 9})
	}
	g = sim.Indexed(g)
	got, want := New(g, 0.5), fullSearch(g, 0.5)
	if !slices.Equal(got.clear, want.clear) || !slices.Equal(got.flags, want.flags) {
		t.Fatal("the grid differs from the full search's")
	}
}
