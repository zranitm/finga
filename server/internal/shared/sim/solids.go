package sim

import (
	"math"
	"slices"
)

// bucketM is the side of a solids bucket (large-regions.md section 5): a body's move or a point reads only the buckets it touches.
const bucketM = 8.0

// blockerM is the side of a blockers bucket: bodies are bucketed by interest cell, so a tick's rebuild clears few buckets.
const blockerM = 32.0

// boundsPad widens a solid's bounds past any rounding in its footprint's tests.
const boundsPad = 1e-6

// solids are a map's boxes and cylinders bucketed by the squares their footprints' bounds overlap.
type solids struct {
	boxes, cylinders buckets
}

// Indexed is g with its boxes and cylinders bucketed, which every query of the sim reads: they come in their own order, each once, so a result is a full scan's to the bit.
func Indexed(g Geometry) Geometry {
	s := &solids{}
	s.boxes.fill(len(g.Boxes), bucketM, g.SizeX, g.SizeZ, func(i int) (x0, z0, x1, z1 float64) {
		b := g.Boxes[i]
		ex := float64(math.Abs(b.UX)*b.HalfW) + float64(math.Abs(b.UZ)*b.HalfD) + boundsPad
		ez := float64(math.Abs(b.UZ)*b.HalfW) + float64(math.Abs(b.UX)*b.HalfD) + boundsPad
		return b.X - ex, b.Z - ez, b.X + ex, b.Z + ez
	})
	s.cylinders.fill(len(g.Cylinders), bucketM, g.SizeX, g.SizeZ, func(i int) (x0, z0, x1, z1 float64) {
		c := g.Cylinders[i]
		return circle(c.X, c.Z, c.Radius)
	})
	g.solids = s
	return g
}

// circle is the bounds of a circle, padded as a solid's are.
func circle(x, z, r float64) (x0, z0, x1, z1 float64) {
	r += boundsPad
	return x - r, z - r, x + r, z + r
}

// Near calls box and cylinder with every solid whose bounds overlap [x0, x1] by [z0, z1], in their own order and each once; g not Indexed gives every one.
func (g Geometry) Near(x0, z0, x1, z1 float64, box func(Box), cylinder func(Cylinder)) {
	n := g.near(false, x0, z0, x1, z1)
	for i, ok := n.next(); ok; i, ok = n.next() {
		box(g.Boxes[i])
	}
	n = g.near(true, x0, z0, x1, z1)
	for i, ok := n.next(); ok; i, ok = n.next() {
		cylinder(g.Cylinders[i])
	}
}

// near walks the boxes', or the cylinders', indices that the box [x0, x1] by [z0, z1] may touch.
func (g Geometry) near(cylinders bool, x0, z0, x1, z1 float64) nearby {
	switch {
	case g.solids == nil && cylinders:
		return nearby{every: len(g.Cylinders), last: -1}
	case g.solids == nil:
		return nearby{every: len(g.Boxes), last: -1}
	case cylinders:
		return g.solids.cylinders.near(len(g.Cylinders), x0, z0, x1, z1)
	}
	return g.solids.boxes.near(len(g.Boxes), x0, z0, x1, z1)
}

// Blockers is a step's blockers bucketed, which Geometry.Blocking lends a geometry so a move reads only those near it. Set rebuilds it in place, so one serves tick after tick.
type Blockers struct {
	list []Blocker // its own copy, so only Set changes what the buckets describe
	buckets
}

// Set buckets a copy of list over a map sizeX by sizeZ, reusing b's arrays.
func (b *Blockers) Set(list []Blocker, sizeX, sizeZ float64) {
	b.list = append(b.list[:0], list...)
	b.fill(len(b.list), blockerM, sizeX, sizeZ, func(i int) (x0, z0, x1, z1 float64) {
		return circle(b.list[i].X, b.list[i].Z, b.list[i].Radius)
	})
}

// Blocking is g stepping against b's blockers, each move reading only those near it; nil blocks nothing.
func (g Geometry) Blocking(b *Blockers) Geometry {
	g.Blockers, g.blockers = nil, b
	if b != nil {
		g.Blockers = b.list
	}
	return g
}

// nearBlockers walks the indices of the blockers whose bounds overlap [x0, x1] by [z0, z1]: from the index while Blockers are its own list, as Blocking lends them, every one if not, as when a move through mobs drops them.
func (g Geometry) nearBlockers(x0, z0, x1, z1 float64) nearby {
	b := g.blockers
	switch {
	case len(g.Blockers) == 0:
		return nearby{last: -1} // as from an index never Set, which has no buckets
	case b == nil || len(b.list) != len(g.Blockers) || &b.list[0] != &g.Blockers[0]:
		return nearby{every: len(g.Blockers), last: -1}
	}
	return b.near(len(b.list), x0, z0, x1, z1)
}

// buckets lists things' indices by the squares their bounds overlap: square k's are items[start[k]:start[k+1]], ascending.
type buckets struct {
	per          float64 // squares a metre: a power of two, so v*per is v/side to the bit
	nx, nz       int
	start, items []int32
}

// fill buckets count things, each over the bounds it gives, in squares of side over a map sizeX by sizeZ, reusing b's arrays.
func (b *buckets) fill(count int, side, sizeX, sizeZ float64, bounds func(i int) (x0, z0, x1, z1 float64)) {
	b.per = 1 / side
	b.nx, b.nz = max(1, int(math.Ceil(sizeX/side))), max(1, int(math.Ceil(sizeZ/side)))
	size := b.nx * b.nz
	b.start = slices.Grow(b.start[:0], size+1)[:size+1]
	clear(b.start)
	total := 0
	for i := range count {
		ix0, ix1, iz0, iz1 := b.cells(bounds(i))
		for iz := iz0; iz <= iz1; iz++ {
			for ix := ix0; ix <= ix1; ix++ {
				b.start[iz*b.nx+ix]++
				total++
			}
		}
	}
	for k := 1; k < size; k++ {
		b.start[k] += b.start[k-1] // each square's end
	}
	b.start[size] = int32(total) //nolint:gosec // a map's things are far fewer than 2³¹
	b.items = slices.Grow(b.items[:0], total)[:total]
	for i := count - 1; i >= 0; i-- { // last first, so each square fills from its end down to its start, ascending
		ix0, ix1, iz0, iz1 := b.cells(bounds(i))
		for iz := iz0; iz <= iz1; iz++ {
			for ix := ix0; ix <= ix1; ix++ {
				k := iz*b.nx + ix
				b.start[k]--
				b.items[b.start[k]] = int32(i) //nolint:gosec // as above
			}
		}
	}
}

// cells are the first and last squares along x and z that [x0, x1] by [z0, z1] overlaps, held to the map: a thing or query past an edge takes the edge's square.
func (b *buckets) cells(x0, z0, x1, z1 float64) (ix0, ix1, iz0, iz1 int) {
	return b.cell(x0, b.nx), b.cell(x1, b.nx), b.cell(z0, b.nz), b.cell(z1, b.nz)
}

func (b *buckets) cell(v float64, n int) int {
	c := math.Floor(v * b.per)
	switch {
	case c < 0 || math.IsNaN(c): // NaN, which no solid's test passes, as in the scan
		return 0
	case c >= float64(n):
		return n - 1
	}
	return int(c)
}

// near walks the indices, of count in all, in the squares the box [x0, x1] by [z0, z1] overlaps: ascending and each once, or every one when it overlaps more squares than a walk holds.
func (b *buckets) near(count int, x0, z0, x1, z1 float64) nearby {
	ix0, ix1, iz0, iz1 := b.cells(x0, z0, x1, z1)
	n := nearby{items: b.items, last: -1}
	if (ix1-ix0+1)*(iz1-iz0+1) > len(n.at) {
		n.every = count
		return n
	}
	for iz := iz0; iz <= iz1; iz++ {
		row := b.start[iz*b.nx+ix0 : iz*b.nx+ix1+2] // each square's start, and the last one's end
		for k := range len(row) - 1 {
			if row[k] < row[k+1] {
				n.at[n.n], n.end[n.n] = row[k], row[k+1]
				n.n++
			}
		}
	}
	return n
}

// nearby walks some squares' ascending runs of items as one, smallest first, skipping an index met in another; with every set, it walks 0 to every-1.
type nearby struct {
	items   []int32
	at, end [16]int32 // each square's run: how far it's read, and its end; up to 4 by 4
	n       int
	every   int
	last    int32
}

func (n *nearby) next() (int, bool) {
	if n.every > 0 {
		if int(n.last)+1 >= n.every {
			return 0, false
		}
		n.last++
		return int(n.last), true
	}
	best, value := -1, int32(0)
	for k := range n.n {
		for n.at[k] < n.end[k] && n.items[n.at[k]] <= n.last {
			n.at[k]++
		}
		if n.at[k] < n.end[k] && (best < 0 || n.items[n.at[k]] < value) {
			best, value = k, n.items[n.at[k]]
		}
	}
	if best < 0 {
		return 0, false
	}
	n.at[best]++
	n.last = value
	return int(value), true
}
