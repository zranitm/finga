package nav

import (
	"math"
	"slices"
)

// reach is how many cells round a too-tight start Path looks for one its body fits.
const reach = 3

// Finder runs A* searches, keeping its buffers between them; one per goroutine, as a map's tick has.
type Finder struct {
	gs     []float64
	parent []int
	closed []bool
	stamp  []uint32 // the search that last touched each window cell; any other leaves it unvisited
	gen    uint32
	open   queue
	skip   []int
}

// Path is the cell centres a body of radius r walks from from to to, to's cell last, within leash of home and through none of avoid's cells, from the nearest cell it fits if pressed into a tight one; nil if there's no way.
func (f *Finder) Path(n *Grid, from, to Point, r float64, home Point, leash float64, avoid ...Point) []Point {
	if math.IsNaN(leash) || math.IsInf(leash, 0) {
		return nil
	}
	start, ok := n.index(from)
	if !ok {
		return nil
	}
	dest, ok := n.index(to)
	if !ok || n.clear[dest] < r || !within(n.centre(dest), home, leash) {
		return nil
	}
	f.skip = f.skip[:0]
	for _, p := range avoid {
		if i, ok := n.index(p); ok && i != start {
			f.skip = append(f.skip, i)
		}
	}
	w := n.window(home, leash)
	if !w.holds(n, start) || slices.Contains(f.skip, dest) {
		return nil
	}
	var lead []Point
	if !n.Fits(from, r) {
		if start, ok = f.nearest(n, w, from, r, home, leash); !ok {
			return nil
		}
		lead = []Point{n.centre(start)}
	}
	if start == dest {
		return append(lead, n.centre(dest))
	}
	f.reset(w.size())
	goal := n.centre(dest)
	k0 := w.at(n, start)
	f.touch(k0)
	f.gs[k0] = 0
	f.open.push(entry{cell: start, f: octile(n.centre(start), goal)})
	for len(f.open) > 0 {
		e := f.open.pop()
		k := w.at(n, e.cell)
		if f.closed[k] {
			continue
		}
		if e.cell == dest {
			return w.trace(n, f.parent, start, dest, lead)
		}
		f.closed[k] = true
		n.moves(e.cell, r, func(j int, diagonal bool) {
			if !w.holds(n, j) || !within(n.centre(j), home, leash) || slices.Contains(f.skip, j) {
				return
			}
			cost := cell
			if diagonal {
				cost = cell * math.Sqrt2
			}
			kj := w.at(n, j)
			f.touch(kj)
			if g := f.gs[k] + cost; g < f.gs[kj] {
				f.gs[kj], f.parent[kj] = g, e.cell
				f.open.push(entry{cell: j, f: g + octile(n.centre(j), goal)})
			}
		})
	}
	return nil
}

// nearest is the cell nearest from, within reach rings of its own, that a body of radius r fits in, inside the leash and not avoided.
func (f *Finder) nearest(n *Grid, w window, from Point, r float64, home Point, leash float64) (int, bool) {
	ix, iz := int(math.Floor(from.X/cell)), int(math.Floor(from.Z/cell))
	best, bestD := -1, math.Inf(1)
	for z := iz - reach; z <= iz+reach; z++ {
		for x := ix - reach; x <= ix+reach; x++ {
			if x < 0 || z < 0 || x >= n.nx || z >= n.nz {
				continue
			}
			i := z*n.nx + x
			c := n.centre(i)
			if d := Distance(from, c); d < bestD && !n.raised[i] && n.clear[i] >= r && w.holds(n, i) && within(c, home, leash) && !slices.Contains(f.skip, i) {
				best, bestD = i, d
			}
		}
	}
	return best, best >= 0
}

// reset readies the buffers for a search over size window cells, growing them only when a window is bigger than any before.
func (f *Finder) reset(size int) {
	if len(f.gs) < size {
		f.gs, f.parent, f.closed, f.stamp, f.gen = make([]float64, size), make([]int, size), make([]bool, size), make([]uint32, size), 0
	}
	if f.gen++; f.gen == 0 {
		clear(f.stamp)
		f.gen = 1
	}
	f.open = f.open[:0]
}

// touch makes window cell k part of this search, unvisited, the first time the search meets it.
func (f *Finder) touch(k int) {
	if f.stamp[k] != f.gen {
		f.stamp[k], f.gs[k], f.parent[k], f.closed[k] = f.gen, math.Inf(1), -1, false
	}
}

// octile is the shortest 8-way distance between two cell centres, so A* never overestimates.
func octile(a, b Point) float64 {
	dx, dz := math.Abs(a.X-b.X), math.Abs(a.Z-b.Z)
	return max(dx, dz) + float64((math.Sqrt2-1)*min(dx, dz))
}

func within(p, home Point, leash float64) bool {
	return Distance(p, home) <= leash
}

// window is the grid's cells inside a leash circle's bounding square, the only ones a search keeps state for.
type window struct{ x0, z0, nx, nz int }

func (n *Grid) window(home Point, leash float64) window {
	x0 := max(int(math.Floor((home.X-leash)/cell)), 0)
	z0 := max(int(math.Floor((home.Z-leash)/cell)), 0)
	x1 := min(int(math.Floor((home.X+leash)/cell)), n.nx-1)
	z1 := min(int(math.Floor((home.Z+leash)/cell)), n.nz-1)
	return window{x0: x0, z0: z0, nx: max(x1-x0+1, 0), nz: max(z1-z0+1, 0)}
}

func (w window) size() int { return w.nx * w.nz }

func (w window) holds(n *Grid, i int) bool {
	x, z := i%n.nx-w.x0, i/n.nx-w.z0
	return x >= 0 && z >= 0 && x < w.nx && z < w.nz
}

func (w window) at(n *Grid, i int) int { return (i/n.nx-w.z0)*w.nx + i%n.nx - w.x0 }

// trace is the way from start to dest, lead first, in one allocation.
func (w window) trace(n *Grid, parent []int, start, dest int, lead []Point) []Point {
	steps := 0
	for c := dest; c != start; c = parent[w.at(n, c)] {
		steps++
	}
	out := make([]Point, len(lead)+steps)
	copy(out, lead)
	k := len(out)
	for c := dest; c != start; c = parent[w.at(n, c)] {
		k--
		out[k] = n.centre(c)
	}
	return out
}

type entry struct {
	cell int
	f    float64
}

// queue is A*'s open set, a binary heap with the lowest estimate on top; typed, so a push doesn't box its entry as container/heap's does.
type queue []entry

func (q *queue) push(e entry) {
	*q = append(*q, e)
	h := *q
	for i := len(h) - 1; i > 0; {
		up := (i - 1) / 2
		if h[up].f <= h[i].f {
			break
		}
		h[up], h[i] = h[i], h[up]
		i = up
	}
}

func (q *queue) pop() entry {
	h := *q
	top := h[0]
	last := len(h) - 1
	h[0] = h[last]
	h = h[:last]
	for i := 0; ; {
		low, l, r := i, 2*i+1, 2*i+2
		if l < len(h) && h[l].f < h[low].f {
			low = l
		}
		if r < len(h) && h[r].f < h[low].f {
			low = r
		}
		if low == i {
			break
		}
		h[i], h[low] = h[low], h[i]
		i = low
	}
	*q = h
	return top
}
