package sim

import "math"

// BucketM is the side of a solids bucket (large-regions.md section 5): a body's move or a point reads only the buckets it touches.
const BucketM = 8.0

// solids buckets a map's boxes and cylinders by the squares their footprints' bounds overlap, each bucket's indices ascending.
type solids struct {
	nx, nz    int
	boxes     [][]int32
	cylinders [][]int32
}

// boundsPad widens a solid's bounds past any rounding in its footprint's tests.
const boundsPad = 1e-6

// Indexed is g with its boxes and cylinders bucketed, which every query of the sim reads: they come in their own order, each once, so a result is a full scan's to the bit.
func Indexed(g Geometry) Geometry {
	s := &solids{nx: buckets(g.SizeX), nz: buckets(g.SizeZ)}
	s.boxes = make([][]int32, s.nx*s.nz)
	s.cylinders = make([][]int32, s.nx*s.nz)
	for i, b := range g.Boxes {
		ex := float64(math.Abs(b.UX)*b.HalfW) + float64(math.Abs(b.UZ)*b.HalfD) + boundsPad
		ez := float64(math.Abs(b.UZ)*b.HalfW) + float64(math.Abs(b.UX)*b.HalfD) + boundsPad
		s.add(s.boxes, i, b.X-ex, b.Z-ez, b.X+ex, b.Z+ez)
	}
	for i, c := range g.Cylinders {
		r := c.Radius + boundsPad
		s.add(s.cylinders, i, c.X-r, c.Z-r, c.X+r, c.Z+r)
	}
	g.solids = s
	return g
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
	count := len(g.Boxes)
	if cylinders {
		count = len(g.Cylinders)
	}
	if g.solids == nil {
		return nearby{every: count, last: -1}
	}
	list := g.solids.boxes
	if cylinders {
		list = g.solids.cylinders
	}
	return g.solids.near(list, count, x0, z0, x1, z1)
}

// buckets is how many buckets cover size metres, one at least.
func buckets(size float64) int { return max(1, int(math.Ceil(size/BucketM))) }

func (s *solids) add(list [][]int32, i int, x0, z0, x1, z1 float64) {
	ix0, ix1 := s.cells(x0, x1, s.nx)
	iz0, iz1 := s.cells(z0, z1, s.nz)
	for iz := iz0; iz <= iz1; iz++ {
		for ix := ix0; ix <= ix1; ix++ {
			k := iz*s.nx + ix
			list[k] = append(list[k], int32(i)) //nolint:gosec // a map's solids are far fewer than 2³¹
		}
	}
}

// cells are the first and last bucket along an axis of n that [lo, hi] overlaps, held to the map: a solid or query past an edge takes the edge's bucket.
func (s *solids) cells(lo, hi float64, n int) (int, int) {
	return clampCell(lo, n), clampCell(hi, n)
}

func clampCell(v float64, n int) int {
	c := math.Floor(v / BucketM)
	switch {
	case c < 0:
		return 0
	case c >= float64(n):
		return n - 1
	}
	return int(c)
}

// near is the boxes' or cylinders' indices, of count in all, in the buckets the box [x0, x1] by [z0, z1] overlaps: ascending and each once, or every one when it overlaps more buckets than a walk holds.
func (s *solids) near(list [][]int32, count int, x0, z0, x1, z1 float64) nearby {
	ix0, ix1 := s.cells(x0, x1, s.nx)
	iz0, iz1 := s.cells(z0, z1, s.nz)
	n := nearby{list: list, last: -1}
	if (ix1-ix0+1)*(iz1-iz0+1) > len(n.cells) {
		n.every = count
		return n
	}
	for iz := iz0; iz <= iz1; iz++ {
		for ix := ix0; ix <= ix1; ix++ {
			if k := iz*s.nx + ix; len(list[k]) > 0 {
				n.cells[n.n] = int32(k) //nolint:gosec // a map's buckets are far fewer than 2³¹
				n.n++
			}
		}
	}
	return n
}

// nearby walks some buckets' ascending lists as one, smallest first, skipping an index met in another; with every set, it walks 0 to every-1.
type nearby struct {
	list  [][]int32
	cells [16]int32 // the buckets walked, up to 4 by 4
	at    [16]int32 // how far each bucket's list is read
	n     int
	every int
	last  int32
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
		l := n.list[n.cells[k]]
		for int(n.at[k]) < len(l) && l[n.at[k]] <= n.last {
			n.at[k]++
		}
		if int(n.at[k]) < len(l) && (best < 0 || l[n.at[k]] < value) {
			best, value = k, l[n.at[k]]
		}
	}
	if best < 0 {
		return 0, false
	}
	n.at[best]++
	n.last = value
	return int(value), true
}
