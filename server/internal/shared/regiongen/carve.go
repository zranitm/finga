package regiongen

import (
	"math"
	"slices"
)

// rockColour is the kit ground a cave's rock and its walls take (underground-and-underwater.md section 4).
const rockColour = "cave_rock"

// wanderM is the size of the noise a carved wall's line wanders by.
const wanderM = 8

// rock raises a cave's ground Rock.Height, keeping the ground's noise as the floor its halls and tunnels are carved back down to.
func (b *builder) rock() {
	r := b.p.Rock
	if r == nil {
		return
	}
	b.floor = slices.Clone(b.h)
	b.rocky = make([]bool, len(b.h))
	for s := range b.h {
		b.h[s] += r.Height
		b.rocky[s] = true
	}
	b.wander = newNoise(int64(b.p.Seed) + 200)
}

// hall carves its area to the floor: the ground's noise, or that ground's mean over it when flat.
func (b *builder) hall(h Hall) {
	in := b.inside(h.Area)
	level := math.NaN()
	if h.Flat {
		sum, n := 0.0, 0
		for s, ok := range in {
			if ok {
				sum, n = sum+b.floor[s], n+1
			}
		}
		level = sum / float64(max(n, 1))
	}
	bed := func(s int) float64 {
		if h.Flat {
			return level
		}
		return b.floor[s]
	}
	reach := b.p.wallReach()
	b.f.near(b.g, h.Area, true, reach)
	for s, ok := range in {
		switch {
		case b.f.has(s):
			d := math.Sqrt(b.f.d2[s])
			if ok {
				d = -d
			}
			b.cut(s, d, bed(s))
		case ok:
			b.cut(s, -reach, bed(s))
		}
	}
}

// tunnel carves its smoothed path Width across to the floor, levelled along it to the floor's mean roadMean each way, as a road's is.
func (b *builder) tunnel(t Tunnel) {
	path := smooth(t.Path)
	s := along(path)
	ground := make([]float64, len(path))
	for i, p := range path {
		ground[i] = b.g.height(b.floor, p[0], p[1])
	}
	levels := make([]float64, len(path))
	for i := range path {
		sum, n := 0.0, 0
		for j := range path {
			if math.Abs(s[j]-s[i]) <= roadMean {
				sum, n = sum+ground[j], n+1
			}
		}
		levels[i] = sum / float64(n)
	}
	half := float64(t.Width / 2)
	b.f.near(b.g, path, false, half+b.p.wallReach())
	for _, x := range b.f.touched {
		level := lerp(levels[b.f.seg[x]], levels[b.f.seg[x]+1], b.f.t[x])
		b.cut(int(x), math.Sqrt(b.f.d2[x])-half, level)
	}
}

// cut carves sample s, d from the carved space's line (negative inside it), down to bed: wholly once the line's wander puts it inside, and up the wall over cliffCells samples beyond, too steep to walk.
func (b *builder) cut(s int, d, bed float64) {
	x, z := b.g.x(s%b.g.nx), b.g.x(s/b.g.nx)
	w := min(max(b.wander.at(x/wanderM, z/wanderM)/0.87, -1), 1)
	d += float64(b.p.Rock.Rough * w)
	if d <= 0 {
		b.rocky[s] = false
	}
	b.carve(s, bed, d/float64(cliffCells*b.g.cell))
}
