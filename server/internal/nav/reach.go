package nav

import (
	"math"

	"finga/internal/sim3d"
)

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
		near, far := n.sides(i, 1), n.sides(i, 2)
		for k, j := range near {
			if j < 0 || n.clears(j)-n.ground[i] > rise {
				continue
			}
			if n.clear[j] >= r {
				visit(j, false)
			}
			if f := far[k]; f >= 0 && n.clear[f] >= r && n.clears(f)-n.ground[i] <= rise && n.clearance(j, n.ground[i]+rise) >= r {
				visit(f, false)
			}
		}
	}
	return Reached{n: n, at: at}
}

// Near reports whether a reached cell's centre lies within d of p.
func (r Reached) Near(p Point, d float64) bool {
	for i, ok := range r.at {
		if c := r.n.centre(i); ok && math.Hypot(c.X-p.X, c.Z-p.Z) <= d {
			return true
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
