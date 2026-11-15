package nav

import (
	"math"
	"slices"
)

// Path is the cell centres a body of radius r walks through from from to to, to's cell last, keeping within leash of home; nil if
// there's no such way. Only cells within the leash are searched, so a search costs at most that circle's cells.
func (n *Grid) Path(from, to Point, r float64, home Point, leash float64) []Point {
	start, ok := n.index(from)
	if !ok {
		return nil
	}
	dest, ok := n.index(to)
	if !ok || n.clear[dest] < r || !within(n.centre(dest), home, leash) {
		return nil
	}
	w := n.window(home, leash)
	if !w.holds(n, start) {
		return nil
	}
	gs, parent, closed := make([]float64, w.size()), make([]int, w.size()), make([]bool, w.size())
	for k := range gs {
		gs[k], parent[k] = math.Inf(1), -1
	}
	goal := n.centre(dest)
	var open queue
	gs[w.at(n, start)] = 0
	open.push(entry{cell: start, f: octile(n.centre(start), goal)})
	for len(open) > 0 {
		e := open.pop()
		k := w.at(n, e.cell)
		if closed[k] {
			continue
		}
		if e.cell == dest {
			return w.trace(n, parent, start, dest)
		}
		closed[k] = true
		n.moves(e.cell, r, func(j int, diagonal bool) {
			if !w.holds(n, j) || !within(n.centre(j), home, leash) {
				return
			}
			cost := Cell
			if diagonal {
				cost = Cell * math.Sqrt2
			}
			kj := w.at(n, j)
			if g := gs[k] + cost; g < gs[kj] {
				gs[kj], parent[kj] = g, e.cell
				open.push(entry{cell: j, f: g + octile(n.centre(j), goal)})
			}
		})
	}
	return nil
}

// octile is the shortest 8-way distance between two cell centres, so A* never overestimates.
func octile(a, b Point) float64 {
	dx, dz := math.Abs(a.X-b.X), math.Abs(a.Z-b.Z)
	return max(dx, dz) + (math.Sqrt2-1)*min(dx, dz)
}

func within(p, home Point, leash float64) bool {
	return math.Hypot(p.X-home.X, p.Z-home.Z) <= leash
}

// window is the grid's cells inside a leash circle's bounding square, the only ones a search keeps state for.
type window struct{ x0, z0, nx, nz int }

func (n *Grid) window(home Point, leash float64) window {
	x0 := max(int(math.Floor((home.X-leash)/Cell)), 0)
	z0 := max(int(math.Floor((home.Z-leash)/Cell)), 0)
	x1 := min(int(math.Floor((home.X+leash)/Cell)), n.NX-1)
	z1 := min(int(math.Floor((home.Z+leash)/Cell)), n.NZ-1)
	return window{x0: x0, z0: z0, nx: max(x1-x0+1, 0), nz: max(z1-z0+1, 0)}
}

func (w window) size() int { return w.nx * w.nz }

func (w window) holds(n *Grid, i int) bool {
	x, z := i%n.NX-w.x0, i/n.NX-w.z0
	return x >= 0 && z >= 0 && x < w.nx && z < w.nz
}

func (w window) at(n *Grid, i int) int { return (i/n.NX-w.z0)*w.nx + i%n.NX - w.x0 }

func (w window) trace(n *Grid, parent []int, start, dest int) []Point {
	var out []Point
	for c := dest; c != start; c = parent[w.at(n, c)] {
		out = append(out, n.centre(c))
	}
	slices.Reverse(out)
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
