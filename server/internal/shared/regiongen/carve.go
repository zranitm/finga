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
	b.past = make([]float64, len(b.h))
	for s := range b.h {
		b.h[s] += r.Height
		b.rocky[s] = true
		b.past[s] = math.Inf(1)
	}
	b.wander = newNoise(int64(b.p.Seed) + 200)
	b.flat = make([]float64, len(b.p.Features))
	for i, f := range b.p.Features {
		b.flat[i] = math.NaN()
		if f.Hall != nil && f.Hall.Flat {
			sum, n := 0.0, 0
			for s, ok := range b.inside(f.Hall.Area) {
				if ok {
					sum, n = sum+b.floor[s], n+1
				}
			}
			b.flat[i] = sum / float64(max(n, 1))
		}
	}
}

// walled is whether sample s is a cave's rock, from a body's radius before its wall's foot: a barrier, as deep water is, since a body walks up a steep wall slantwise.
func (b *builder) walled(s int) bool {
	return b.past != nil && b.past[s] > -b.radius
}

// hallFloor is feature i, a hall, carved to at (x, z): its level when flat, else the ground under the rock.
func (b *builder) hallFloor(i int, x, z float64) float64 {
	if !math.IsNaN(b.flat[i]) {
		return b.flat[i]
	}
	return b.g.height(b.floor, x, z)
}

// hall carves feature i's area to the floor: the ground's noise, or that ground's mean over it when flat.
func (b *builder) hall(i int, h Hall) {
	in := b.inside(h.Area)
	bed := func(s int) float64 {
		if h.Flat {
			return b.flat[i]
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

// carved is a tunnel as carved: its smoothed path, each point's level, and half its width.
type carved struct {
	path   [][2]float64
	levels []float64
	half   float64
}

// levelAt is the tunnel's floor at at, if at lies within its width.
func (c carved) levelAt(at [2]float64) (float64, bool) {
	best, level := math.Inf(1), 0.0
	for i := 0; i+1 < len(c.path); i++ {
		if d2, t := segment(at[0], at[1], c.path[i], c.path[i+1]); d2 < best {
			best, level = d2, lerp(c.levels[i], c.levels[i+1], t)
		}
	}
	return level, best <= float64(c.half*c.half)
}

// spaceFloor is the lowest floor of the spaces at holds, a hall anywhere in the plan or a tunnel carved before; +Inf in none.
func (b *builder) spaceFloor(at [2]float64) float64 {
	floor := math.Inf(1)
	for i, f := range b.p.Features {
		if f.Hall != nil && contains(f.Hall.Area, at) {
			floor = min(floor, b.hallFloor(i, at[0], at[1]))
		}
	}
	for _, c := range b.tunnels {
		if l, ok := c.levelAt(at); ok {
			floor = min(floor, l)
		}
	}
	return floor
}

// join holds a tunnel's levels to the floor of each space they lie in, and the rest within half the walkable slope of those.
func (b *builder) join(path [][2]float64, s, levels []float64) {
	pinned := make([]bool, len(path))
	for i, at := range path {
		if floor := b.spaceFloor(at); !math.IsInf(floor, 1) {
			levels[i], pinned[i] = floor, true
		}
	}
	ramp := b.maxSlope / 2
	for i := range levels {
		if pinned[i] {
			continue
		}
		lo, hi := math.Inf(-1), math.Inf(1)
		for j := range levels {
			if pinned[j] {
				d := float64(ramp * math.Abs(s[i]-s[j]))
				lo, hi = max(lo, levels[j]-d), min(hi, levels[j]+d)
			}
		}
		if lo > hi { // two joins too far apart in height for the ramp: halfway between
			levels[i] = (lo + hi) / 2
			continue
		}
		levels[i] = min(max(levels[i], lo), hi)
	}
}

// tunnel carves its smoothed path Width across, levelled as a road is, joined to the spaces it meets and never below their floors in them.
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
	b.join(path, s, levels)
	b.f.near(b.g, path, false, half+b.p.wallReach())
	for _, x := range b.f.touched {
		level := lerp(levels[b.f.seg[x]], levels[b.f.seg[x]+1], b.f.t[x])
		if floor := b.spaceFloor([2]float64{b.g.x(int(x) % b.g.nx), b.g.x(int(x) / b.g.nx)}); !math.IsInf(floor, 1) {
			level = max(level, floor)
		}
		b.cut(int(x), math.Sqrt(b.f.d2[x])-half, level)
	}
	b.tunnels = append(b.tunnels, carved{path: path, levels: levels, half: half})
}

// cut carves sample s, d from the carved space's line (negative inside it), down to bed: wholly once the line's wander puts it inside, then up the wall over cliffCells samples.
func (b *builder) cut(s int, d, bed float64) {
	x, z := b.g.x(s%b.g.nx), b.g.x(s/b.g.nx)
	w := min(max(b.wander.at(x/wanderM, z/wanderM)/0.87, -1), 1)
	d += float64(b.p.Rock.Rough * w)
	b.past[s] = min(b.past[s], d)
	if d <= 0 {
		b.rocky[s] = false
	}
	b.carve(s, bed, d/float64(cliffCells*b.g.cell))
}
