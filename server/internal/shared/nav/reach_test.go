package nav

import (
	"math"

	"finga/internal/shared/sim"
)

// The reach check's flood is test code: it runs over the shipped maps once per test run, not on every load.

// Reached is the cells a player reaches, from Reach.
type Reached struct {
	n  *Grid
	at []bool
}

// Reach floods from starts, walking as A* does, and jumping or dropping one or two cells along an axis if the feet rise at most rise.
func (n *Grid) Reach(starts []Point, r, rise float64) Reached {
	at := make([]bool, n.nx*n.nz)
	var queue []int
	visit := func(j int, _ bool) {
		if !at[j] {
			at[j] = true
			queue = append(queue, j)
		}
	}
	for _, p := range starts {
		if i, ok := n.index(p); ok {
			visit(i, false)
		}
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		n.moves(i, r, visit)
		for k, j := range n.sides(i) {
			if j < 0 || n.clears(j)-n.spot(i).ground > rise {
				continue
			}
			if n.fits(j, r) {
				visit(j, false)
				continue // a body fits on j, so whatever lies beyond it is reached from there
			}
			// j is too tight to stand in, as beside a solid, so the jump goes on to f if j has room for the body at the jump's height
			if f := n.sides(j)[k]; f >= 0 && n.fits(f, r) && n.clears(f)-n.spot(i).ground <= rise && clearance(n.g, n.step, n.centre(j), n.spot(i).ground+rise) >= r {
				visit(f, false)
			}
		}
	}
	return Reached{n: n, at: at}
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

// clears is how high the feet must be to come over b in the air, as the sim decides: a solid's top less step, or the terrain's top.
func (n *Grid) clears(b int) float64 {
	if s := n.spot(b); s.onTop {
		return s.ground - n.step
	}
	c := n.centre(b)
	return n.g.Terrain.Top(c.X, c.Z)
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
