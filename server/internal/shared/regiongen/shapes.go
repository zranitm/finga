package regiongen

import (
	"math"
	"slices"
)

// grid is a region's terrain samples, nx by nz, cell apart, row by row from z = 0.
type grid struct {
	nx, nz int
	cell   float64
}

func (g grid) x(ix int) float64 { return float64(float64(ix) * g.cell) }

// index is the sample at (ix, iz).
func (g grid) index(ix, iz int) int { return iz*g.nx + ix }

// nearest is the sample nearest (x, z), held to the grid.
func (g grid) nearest(x, z float64) int {
	return g.index(g.clampX(int(math.Floor(x/g.cell+0.5))), g.clampZ(int(math.Floor(z/g.cell+0.5))))
}

func (g grid) clampX(ix int) int { return min(max(ix, 0), g.nx-1) }

func (g grid) clampZ(iz int) int { return min(max(iz, 0), g.nz-1) }

// span is the samples' indices along one axis from lo to hi, held to the grid; first > last when none.
func span(lo, hi, cell float64, n int) (int, int) {
	return max(int(math.Ceil(lo/cell)), 0), min(int(math.Floor(hi/cell)), n-1)
}

// height is h between samples, bilinear, as sim.Terrain reads it.
func (g grid) height(h []float64, x, z float64) float64 {
	fx, fz := min(max(x/g.cell, 0), float64(g.nx-1)), min(max(z/g.cell, 0), float64(g.nz-1))
	ix, iz := min(int(fx), g.nx-2), min(int(fz), g.nz-2)
	tx, tz := fx-float64(ix), fz-float64(iz)
	h00, h10 := h[g.index(ix, iz)], h[g.index(ix+1, iz)]
	h01, h11 := h[g.index(ix, iz+1)], h[g.index(ix+1, iz+1)]
	south, north := h00+float64(tx*(h10-h00)), h01+float64(tx*(h11-h01))
	return south + float64(tz*(north-south))
}

// across is how far the ray from at along unit dir runs before it crosses an edge of poly, those skip names left out; +Inf if it crosses none.
func across(poly [][2]float64, at, dir [2]float64, skip func(edge int) bool) float64 {
	best := math.Inf(1)
	for i, a := range poly {
		if skip(i) {
			continue
		}
		b := poly[(i+1)%len(poly)]
		ex, ez := b[0]-a[0], b[1]-a[1]
		den := float64(dir[0]*ez) - float64(dir[1]*ex)
		if den == 0 {
			continue
		}
		wx, wz := a[0]-at[0], a[1]-at[1]
		t := (float64(wx*ez) - float64(wz*ex)) / den
		u := (float64(wx*dir[1]) - float64(wz*dir[0])) / den
		if t > 0 && u >= 0 && u <= 1 {
			best = min(best, t)
		}
	}
	return best
}

// contains is whether at lies inside poly, by fill's rule: an edge's lower end counted and its upper not.
func contains(poly [][2]float64, at [2]float64) bool {
	in := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		if (a[1] <= at[1]) != (b[1] <= at[1]) {
			t := (at[1] - a[1]) / (b[1] - a[1])
			if a[0]+float64(t*(b[0]-a[0])) < at[0] {
				in = !in
			}
		}
	}
	return in
}

// fill calls in for every sample inside poly, row by row: a row's samples between pairs of its crossings, each edge's lower end counted and its upper not.
func (g grid) fill(poly [][2]float64, in func(s int)) {
	z0, z1 := poly[0][1], poly[0][1]
	for _, p := range poly {
		z0, z1 = min(z0, p[1]), max(z1, p[1])
	}
	iz0, iz1 := span(z0, z1, g.cell, g.nz)
	var xs []float64
	for iz := iz0; iz <= iz1; iz++ {
		z := g.x(iz)
		xs = xs[:0]
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			if (a[1] <= z) != (b[1] <= z) {
				t := (z - a[1]) / (b[1] - a[1])
				xs = append(xs, a[0]+float64(t*(b[0]-a[0])))
			}
		}
		slices.Sort(xs)
		for k := 0; k+1 < len(xs); k += 2 {
			ix0, ix1 := span(xs[k], xs[k+1], g.cell, g.nx)
			for ix := ix0; ix <= ix1; ix++ {
				if x := g.x(ix); x >= xs[k] && x < xs[k+1] {
					in(g.index(ix, iz))
				}
			}
		}
	}
}

// field holds, for the samples within reach of a path this pass, the nearest segment, how far along it, and the squared distance; a sample's stamp says whether this pass set it.
type field struct {
	d2      []float64
	t       []float64
	seg     []int32
	stamp   []int32
	pass    int32
	touched []int32 // this pass's samples, in the order first reached
}

func newField(n int) *field {
	return &field{d2: make([]float64, n), t: make([]float64, n), seg: make([]int32, n), stamp: make([]int32, n)}
}

// near starts a pass over path's segments, the closing one too when closed, reaching reach from them.
func (f *field) near(g grid, path [][2]float64, closed bool, reach float64) {
	f.pass++
	f.touched = f.touched[:0]
	r2 := float64(reach * reach)
	n := len(path) - 1
	if closed {
		n = len(path)
	}
	for i := range n {
		a, b := path[i], path[(i+1)%len(path)]
		ix0, ix1 := span(min(a[0], b[0])-reach, max(a[0], b[0])+reach, g.cell, g.nx)
		iz0, iz1 := span(min(a[1], b[1])-reach, max(a[1], b[1])+reach, g.cell, g.nz)
		for iz := iz0; iz <= iz1; iz++ {
			for ix := ix0; ix <= ix1; ix++ {
				d2, t := segment(g.x(ix), g.x(iz), a, b)
				s := g.index(ix, iz)
				if d2 > r2 || (f.stamp[s] == f.pass && d2 >= f.d2[s]) {
					continue
				}
				if f.stamp[s] != f.pass {
					f.stamp[s] = f.pass
					f.touched = append(f.touched, int32(s)) //nolint:gosec // check caps samples at 2²⁴
				}
				f.d2[s], f.t[s], f.seg[s] = d2, t, int32(i) //nolint:gosec // a path's segments are far fewer than 2³¹
			}
		}
	}
}

// has is whether this pass reached sample s.
func (f *field) has(s int) bool { return f.stamp[s] == f.pass }

// segment is the squared distance from (x, z) to the segment from a to b, and how far along it the nearest point lies, 0 to 1.
func segment(x, z float64, a, b [2]float64) (float64, float64) {
	dx, dz := b[0]-a[0], b[1]-a[1]
	t := 0.0
	if l2 := float64(dx*dx) + float64(dz*dz); l2 > 0 {
		t = min(max((float64((x-a[0])*dx)+float64((z-a[1])*dz))/l2, 0), 1)
	}
	ex, ez := x-(a[0]+float64(t*dx)), z-(a[1]+float64(t*dz))
	return float64(ex*ex) + float64(ez*ez), t
}

// onPath is whether at lies within reach of path.
func onPath(path [][2]float64, at [2]float64, reach float64) bool {
	for i := 0; i+1 < len(path); i++ {
		if d2, _ := segment(at[0], at[1], path[i], path[i+1]); d2 <= float64(reach*reach) {
			return true
		}
	}
	return false
}

// smooth is path cut at a quarter and three quarters of each segment, smoothing times (Chaikin's method), its ends kept.
func smooth(path [][2]float64) [][2]float64 {
	for range smoothing {
		out := [][2]float64{path[0]}
		for i := 0; i+1 < len(path); i++ {
			a, b := path[i], path[i+1]
			out = append(out, lerp2(a, b, 0.25), lerp2(a, b, 0.75))
		}
		out = append(out, path[len(path)-1])
		path = out
	}
	return path
}

func lerp2(a, b [2]float64, t float64) [2]float64 {
	return [2]float64{lerp(a[0], b[0], t), lerp(a[1], b[1], t)}
}

// lerp is a + t(b − a), its product rounded.
func lerp(a, b, t float64) float64 { return a + float64(t*(b-a)) }

// smoothstep is 3t² − 2t³ for t held to 0 to 1: flat at both ends.
func smoothstep(t float64) float64 {
	t = min(max(t, 0), 1)
	return float64(float64(t*t) * (3 - float64(2*t)))
}

// along is each point's distance along path from its start.
func along(path [][2]float64) []float64 {
	s := make([]float64, len(path))
	for i := 1; i < len(path); i++ {
		dx, dz := path[i][0]-path[i-1][0], path[i][1]-path[i-1][1]
		s[i] = s[i-1] + math.Sqrt(float64(dx*dx)+float64(dz*dz))
	}
	return s
}

// f32 is v rounded to a float32, as the bake stores it.
func f32(v float64) float64 { return float64(float32(v)) }
