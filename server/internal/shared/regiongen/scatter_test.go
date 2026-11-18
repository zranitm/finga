package regiongen

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// Poisson disc points lie in their box, no two closer than r, and leave no gap a point would fit in: every spot of the box lies within 2r of one.
func TestPoissonPointsAreApartAndLeaveNoGap(t *testing.T) {
	for _, c := range []struct {
		lo, hi [2]float64
		r      float64
	}{
		{[2]float64{0, 0}, [2]float64{100, 60}, 4},
		{[2]float64{10.5, 20.25}, [2]float64{40, 90}, 2.5},
		{[2]float64{0, 0}, [2]float64{3, 3}, 9}, // smaller than one point's room
	} {
		pts := poisson(rand.NewPCG(7, 1), c.lo, c.hi, c.r)
		for i, p := range pts {
			if p[0] < c.lo[0] || p[1] < c.lo[1] || p[0] > c.hi[0] || p[1] > c.hi[1] {
				t.Fatalf("point %v outside %v to %v", p, c.lo, c.hi)
			}
			for _, q := range pts[:i] {
				if d := dist(p[0]-q[0], p[1]-q[1]); d < c.r {
					t.Fatalf("points %v and %v %v apart; want %v at least", p, q, d, c.r)
				}
			}
		}
		for x := c.lo[0]; x <= c.hi[0]; x += 0.5 {
			for z := c.lo[1]; z <= c.hi[1]; z += 0.5 {
				covered := false
				for _, p := range pts {
					covered = covered || dist(p[0]-x, p[1]-z) <= 2*c.r
				}
				if !covered {
					t.Fatalf("(%v, %v) is %v from every point; a point would fit there", x, z, 2*c.r)
				}
			}
		}
	}
}

// Scatter keeps to its rules: each model in its own biome, its spacing apart, on walkable ground no steeper than its slope, inside the region, off and away from roads, water, camps, towns and spots by its distances, and no solid within half a metre of another.
func TestScatterKeepsItsRules(t *testing.T) {
	p := small(t)
	r := smallRegion(t)
	g := grid{nx: r.Geometry.Terrain.NX, nz: r.Geometry.Terrain.NZ, cell: p.Cell}
	b := &builder{p: p, g: g, h: r.Geometry.Terrain.Heights}
	b.areas()
	slack := math.Sqrt2 / 2 * p.Cell // distances are judged at the nearest sample
	type rule struct {
		biome string
		Scatter
	}
	rules := map[string]rule{}
	for name, bm := range p.Biomes {
		for _, sc := range bm.Scatter {
			rules[sc.Model] = rule{name, sc}
		}
	}
	k := shippedKit(t)
	var roads, rivers [][][2]float64
	var widths, riverWidths []float64
	for _, f := range p.Features {
		if f.Road != nil {
			roads, widths = append(roads, smooth(f.Road.Path)), append(widths, f.Road.Width)
		}
		if f.River != nil {
			rivers, riverWidths = append(rivers, smooth(f.River.Path)), append(riverWidths, f.River.Width)
		}
	}
	pathDist := func(path [][2]float64, x, z float64) float64 {
		best := math.Inf(1)
		for i := 0; i+1 < len(path); i++ {
			d2, _ := segment(x, z, path[i], path[i+1])
			best = min(best, math.Sqrt(d2))
		}
		return best
	}
	counts := map[string]int{}
	var solids [][3]float64
	for _, pr := range r.Props[len(p.Pieces):] {
		model := r.Models[pr.Model]
		rl, ok := rules[model]
		if !ok {
			t.Fatalf("prop %s isn't a scatter's model", model)
		}
		counts[model]++
		reach := k.Models[model].reach()
		s := g.nearest(pr.X, pr.Z)
		ix, iz := s%g.nx, s/g.nx
		switch {
		case b.biomeOf(s) != rl.biome:
			t.Fatalf("%s at (%v, %v) in biome %s; want %s", model, pr.X, pr.Z, b.biomeOf(s), rl.biome)
		case rl.Slope > 0 && math.Sqrt(b.slope2(ix, iz)) > rl.Slope+1e-9:
			t.Fatalf("%s at (%v, %v) on a slope of %v; want %v at most", model, pr.X, pr.Z, math.Sqrt(b.slope2(ix, iz)), rl.Slope)
		case b.steepAt(r.Geometry.Terrain, ix, iz):
			t.Fatalf("%s at (%v, %v) on ground too steep to walk", model, pr.X, pr.Z)
		case pr.X < reach || pr.Z < reach || pr.X > p.Size[0]-reach || pr.Z > p.Size[1]-reach:
			t.Fatalf("%s at (%v, %v) reaches out of the region", model, pr.X, pr.Z)
		}
		for i, rd := range roads {
			if d := pathDist(rd, pr.X, pr.Z) - widths[i]/2; d <= rl.Away.Road+reach-slack {
				t.Fatalf("%s at (%v, %v) %v m from a road's edge; want over %v", model, pr.X, pr.Z, d, rl.Away.Road+reach)
			}
		}
		for i, rv := range rivers {
			if d := pathDist(rv, pr.X, pr.Z) - riverWidths[i]/2; d <= rl.Away.Water+reach-slack {
				t.Fatalf("%s at (%v, %v) %v m from a river's edge; want over %v", model, pr.X, pr.Z, d, rl.Away.Water+reach)
			}
		}
		for _, c := range p.Camps {
			if d := dist(pr.X-c.At[0], pr.Z-c.At[1]) - c.Radius; d <= rl.Away.Camp+reach-slack {
				t.Fatalf("%s at (%v, %v) %v m from camp %s; want over %v", model, pr.X, pr.Z, d, c.Name, rl.Away.Camp+reach)
			}
		}
		for _, sp := range p.spots() {
			if d := dist(pr.X-sp.at[0], pr.Z-sp.at[1]) - sp.radius; d <= rl.Away.Waypoint+reach-slack {
				t.Fatalf("%s at (%v, %v) %v m from %s; want over %v", model, pr.X, pr.Z, d, sp.what, rl.Away.Waypoint+reach)
			}
		}
		for _, a := range p.Areas {
			if a.Kind == Town && insidePolygon(a.Area, pr.X, pr.Z) {
				t.Fatalf("%s at (%v, %v) in town %s", model, pr.X, pr.Z, a.ID)
			}
		}
		if r.Wet[s] {
			t.Fatalf("%s at (%v, %v) in water", model, pr.X, pr.Z)
		}
		for _, f := range p.Features {
			if f.Lake == nil {
				continue
			}
			ring := append(slices.Clone(f.Lake.Area), f.Lake.Area[0])
			if d := pathDist(ring, pr.X, pr.Z); insidePolygon(f.Lake.Area, pr.X, pr.Z) || d <= rl.Away.Water+reach-slack {
				t.Fatalf("%s at (%v, %v) %v m from a lake's edge; want over %v", model, pr.X, pr.Z, d, rl.Away.Water+reach)
			}
		}
		for _, q := range r.Props[len(p.Pieces):] {
			if q != pr && r.Models[q.Model] == model && dist(pr.X-q.X, pr.Z-q.Z) < rl.Spacing-1e-3 {
				t.Fatalf("two %s %v apart; want %v at least", model, dist(pr.X-q.X, pr.Z-q.Z), rl.Spacing)
			}
		}
		if reach > 0 {
			for _, o := range solids {
				if dist(pr.X-o[0], pr.Z-o[1]) < reach+o[2]+solidGap-1e-3 {
					t.Fatalf("%s at (%v, %v) within %v of a solid at (%v, %v)", model, pr.X, pr.Z, solidGap, o[0], o[1])
				}
			}
		}
		solids = append(solids, [3]float64{pr.X, pr.Z, reach})
	}
	for model := range rules {
		if counts[model] == 0 {
			t.Errorf("no %s scattered", model)
		}
	}
}

// Scatter goes round set pieces: no scattered solid stands within half a metre of one.
func TestScatterGoesRoundSetPieces(t *testing.T) {
	p := small(t)
	r := smallRegion(t)
	k := shippedKit(t)
	for _, pc := range r.Props[:len(p.Pieces)] {
		pr := k.Models[r.Models[pc.Model]].reach()
		for _, q := range r.Props[len(p.Pieces):] {
			if qr := k.Models[r.Models[q.Model]].reach(); qr > 0 && dist(pc.X-q.X, pc.Z-q.Z) < pr+qr+solidGap-1e-3 {
				t.Fatalf("%s at (%v, %v) crowds the set piece %s at (%v, %v)", r.Models[q.Model], q.X, q.Z, r.Models[pc.Model], pc.X, pc.Z)
			}
		}
	}
}

func dist(dx, dz float64) float64 { return math.Sqrt(dx*dx + dz*dz) }

// insidePolygon is the even-odd test.
func insidePolygon(poly [][2]float64, x, z float64) bool {
	in := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		if (a[1] <= z) != (b[1] <= z) && x < a[0]+(z-a[1])/(b[1]-a[1])*(b[0]-a[0]) {
			in = !in
		}
	}
	return in
}

// A wide set piece keeps scatter off all of it: no tree stands within half a metre of a large house's reach, however many squares away its middle lies.
func TestScatterKeepsOffAWideSetPiece(t *testing.T) {
	p := flat()
	p.Pieces = []Piece{{House: &House{Area: [4]float64{58, 52, 11, 14.95}, Height: 14, Model: "house_large"}}}
	p.Biomes["meadow"] = Biome{Colours: p.Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "tree_oak", Spacing: 2}}}
	r := build(t, p)
	k := shippedKit(t)
	house, tree := k.Models["house_large"].reach(), k.Models["tree_oak"].reach()
	hx, hz := 58+11.0/2, 52+14.95/2
	trees := 0
	for _, pr := range r.Props[1:] {
		trees++
		if d := dist(pr.X-hx, pr.Z-hz); d < house+tree+solidGap-1e-3 {
			t.Fatalf("a tree at (%v, %v), %v from the house's middle; want %v at least", pr.X, pr.Z, d, house+tree+solidGap)
		}
	}
	if trees < 1000 {
		t.Fatalf("%d trees; want the meadow full of them", trees)
	}
}
