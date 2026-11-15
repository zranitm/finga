package nav

import (
	"math"

	"finga/internal/sim3d"
)

// The reach check's flood: tests only, run on the shipped maps once, since a flood on every data load made the loads heavy.

// Reached is the cells a player reaches, from Reach.
type Reached struct {
	n  *Grid
	at []bool
}

// Reach floods the grid from starts for a player of radius r: walking as A* does, and jumping or dropping a cell or two along x or z
// when the feet must rise at most rise to come over each cell. Two cells, so a body can take off from beside a solid, or land
// there: that cell's centre is nearer the solid than the body's radius, but the body stands in it touching the solid, as in the sim.
// The cell jumped over needs room for the body among the solids too tall to clear, so a gap narrower than the body stays shut.
func (n *Grid) Reach(starts []Point, r, rise float64) Reached {
	at := make([]bool, n.NX*n.NZ)
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
			if j < 0 || n.clears(j)-n.ground[i] > rise {
				continue
			}
			if n.clear[j] >= r {
				visit(j, false)
				continue // a body fits on j, so whatever lies beyond it is reached from there
			}
			if f := n.sides(j)[k]; f >= 0 && n.clear[f] >= r && n.clears(f)-n.ground[i] <= rise && clearance(n.g, n.step, n.centre(j), n.ground[i]+rise) >= r {
				visit(f, false)
			}
		}
	}
	return Reached{n: n, at: at}
}

// clears is how high the feet must be to come over cell b in the air: a solid's top less step, as the sim stops a body only at a
// higher one, or a terrain cell's highest sample, as it lets a body into a steep one only from there.
func (n *Grid) clears(b int) float64 {
	if n.onTop[b] {
		return n.ground[b] - n.step
	}
	c := n.centre(b)
	return n.g.Terrain.Top(c.X, c.Z)
}

// Near reports whether a reached cell's centre lies within d of p.
func (r Reached) Near(p Point, d float64) bool {
	n := r.n
	x0, x1 := max(int(math.Floor((p.X-d)/Cell)), 0), min(int(math.Floor((p.X+d)/Cell)), n.NX-1)
	z0, z1 := max(int(math.Floor((p.Z-d)/Cell)), 0), min(int(math.Floor((p.Z+d)/Cell)), n.NZ-1)
	for z := z0; z <= z1; z++ {
		for x := x0; x <= x1; x++ {
			if i := z*n.NX + x; r.at[i] && math.Hypot(n.centre(i).X-p.X, n.centre(i).Z-p.Z) <= d {
				return true
			}
		}
	}
	return false
}

// JumpRise is how far a jump from flat ground lifts the feet, played by the sim's own steps, which peak a little lower as the
// speeds of p rise.
func JumpRise(p sim3d.Params) float64 {
	g := sim3d.Geometry{SizeX: 2, SizeZ: 2, Terrain: sim3d.NewTerrain(2, 2, 2, []float64{0, 0, 0, 0}, p.MaxSlope)}
	s := sim3d.Step(sim3d.State{X: 1, Z: 1, OnGround: true}, sim3d.Input{Jump: true}, g, p)
	top := s.Y
	for !s.OnGround {
		s = sim3d.Step(s, sim3d.Input{}, g, p)
		top = max(top, s.Y)
	}
	return top
}
