package nav

import (
	"math"
	"slices"

	"finga/internal/shared/sim"
)

// The reach check's flood is test code: it runs over the shipped maps once per test run, not on every load.

// Reached is the cells a player reaches, from Reach.
type Reached struct {
	n  *Grid
	at []bool
}

// Reach floods from starts by a player's steps.
func (n *Grid) Reach(starts []Point, r, rise float64) Reached {
	return n.ReachWithout(starts, r, rise, nil)
}

// ReachWithout is Reach that never enters a cell whose centre lies in without, as if it were gone.
func (n *Grid) ReachWithout(starts []Point, r, rise float64, without func(Point) bool) Reached {
	lv := n.levels()
	at := make([]bool, n.nx*n.nz)
	var queue []int
	visit := func(j int) {
		if !at[j] && (without == nil || !without(n.centre(j))) {
			at[j] = true
			queue = append(queue, j)
		}
	}
	for _, p := range starts {
		if i, ok := n.index(p); ok {
			visit(i)
		}
	}
	for head := 0; head < len(queue); head++ {
		n.steps(queue[head], r, rise, lv, visit)
	}
	return Reached{n: n, at: at}
}

// steps calls visit with each cell a player goes to from cell i: walking as A* does, and jumping or dropping one or two cells along an axis if the feet rise at most rise, never onto a barrier from off one, as the sim stops a body at any height.
func (n *Grid) steps(i int, r, rise float64, lv levels, visit func(j int)) {
	n.moves(i, r, func(j int, _ bool) { visit(j) })
	dry, ground := n.flags[i]&barred == 0, lv.ground[i]
	for k, j := range n.sides(i) {
		if j < 0 || lv.over[j]-ground > rise || dry && n.flags[j]&barred != 0 {
			continue
		}
		if n.fits(j, r) {
			visit(j)
			continue // a body fits on j, so whatever lies beyond it is reached from there
		}
		// j is too tight to stand in, as beside a solid, so the jump goes on to f if j has room for the body at the jump's height
		if f := n.sides(j)[k]; f >= 0 && n.flags[f]&barred == 0 && n.fits(f, r) && lv.over[f]-ground <= rise && clearance(n.g, n.step, n.centre(j), ground+rise) >= r {
			visit(f)
		}
	}
}

// levels are each cell's ground and how high the feet must be to come over it in the air, as the sim decides: a solid's top less step, or the terrain's top. Flat tables a flood reads without a lock.
type levels struct{ ground, over []float64 }

var levelsOf = map[*Grid]levels{}

// levels are n's, worked out once: the ground as stand finds it, with none of its clearance.
func (n *Grid) levels() levels {
	spotsMu.Lock()
	defer spotsMu.Unlock()
	if lv, ok := levelsOf[n]; ok {
		return lv
	}
	lv := levels{ground: make([]float64, n.nx*n.nz), over: make([]float64, n.nx*n.nz)}
	for i := range lv.ground {
		c := n.centre(i)
		ground := sim.Ground(c.X, c.Z, math.Inf(1), n.g)
		lv.ground[i], lv.over[i] = ground, n.g.Terrain.Top(c.X, c.Z)
		if ground > n.g.Terrain.Height(c.X, c.Z) { // on a solid's top
			lv.over[i] = ground - n.step
		}
	}
	levelsOf[n] = lv
	return lv
}

// Stranded are the cells Reach reaches from start that lead back to it by no steps: one-way pockets, dropped into and never left, by their centres.
func (n *Grid) Stranded(start Point, r, rise float64) []Point {
	ahead, lv := n.Reach([]Point{start}, r, rise), n.levels()
	// every cell a step from a cell may go to, as an offset
	ways := [...]int{-1, 1, -n.nx, n.nx, -1 - n.nx, 1 - n.nx, -1 + n.nx, 1 + n.nx, -2, 2, -2 * n.nx, 2 * n.nx}
	out := make([]uint16, len(ahead.at)) // bit k: a step from the cell goes ways[k] on
	for i, in := range ahead.at {
		if in {
			n.steps(i, r, rise, lv, func(j int) { out[i] |= 1 << slices.Index(ways[:], j-i) })
		}
	}
	back := make([]bool, len(ahead.at))
	s, _ := n.index(start)
	back[s] = true
	queue := []int{s}
	for len(queue) > 0 {
		j := queue[0]
		queue = queue[1:]
		for k, d := range ways {
			if i := j - d; i >= 0 && i < len(out) && !back[i] && out[i]&(1<<k) != 0 {
				back[i] = true
				queue = append(queue, i)
			}
		}
	}
	var stranded []Point
	for i, in := range ahead.at {
		if in && !back[i] {
			stranded = append(stranded, n.centre(i))
		}
	}
	return stranded
}

// WalkWithin floods from start as a mob walks, a body of radius r on cells inside leash of home, as its A* keeps to.
func (n *Grid) WalkWithin(start Point, r float64, home Point, leash float64) Reached {
	at := make([]bool, n.nx*n.nz)
	i, ok := n.index(start)
	if !ok || !n.fits(i, r) {
		return Reached{n: n, at: at}
	}
	at[i] = true
	queue := []int{i}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		n.moves(c, r, func(j int, _ bool) {
			if !at[j] && within(n.centre(j), home, leash) {
				at[j] = true
				queue = append(queue, j)
			}
		})
	}
	return Reached{n: n, at: at}
}

// Has reports whether p's cell was reached.
func (r Reached) Has(p Point) bool {
	i, ok := r.n.index(p)
	return ok && r.at[i]
}

// Near reports whether a reached cell's centre lies within d of p.
func (r Reached) Near(p Point, d float64) bool {
	n := r.n
	x0, x1 := max(int(math.Floor((p.X-d)/cell)), 0), min(int(math.Floor((p.X+d)/cell)), n.nx-1)
	z0, z1 := max(int(math.Floor((p.Z-d)/cell)), 0), min(int(math.Floor((p.Z+d)/cell)), n.nz-1)
	for z := z0; z <= z1; z++ {
		for x := x0; x <= x1; x++ {
			if i := z*n.nx + x; r.at[i] && Distance(n.centre(i), p) <= d {
				return true
			}
		}
	}
	return false
}

// JumpRise is how far the sim's own jump lifts the feet from flat ground; its steps peak a little lower as p's speeds rise.
func JumpRise(p sim.Params) float64 {
	g := sim.Geometry{SizeX: 2, SizeZ: 2, Terrain: sim.NewTerrain(2, 2, 2, []float64{0, 0, 0, 0}, p.MaxSlope)}
	s := sim.Step(sim.State{X: 1, Z: 1, OnGround: true}, sim.Input{Jump: true}, g, p)
	top := s.Y
	for !s.OnGround {
		s = sim.Step(s, sim.Input{}, g, p)
		top = max(top, s.Y)
	}
	return top
}
