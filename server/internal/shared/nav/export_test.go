package nav

import "math"

// MaxClear is the most clearance a cell holds.
const MaxClear = maxClear

// GroundAt is the ground of p's cell, NaN off the map.
func (n *Grid) GroundAt(p Point) float64 {
	if i, ok := n.index(p); ok {
		return n.spot(i).ground
	}
	return math.NaN()
}

// spot is cell i's ground and clearance, as the build found them.
func (n *Grid) spot(i int) spot { return stand(n.g, n.step, n.centre(i)) }

// Cells are every cell's clearance and flags, which a spot's verdict and every walk rest on.
func (n *Grid) Cells() (clear []uint16, flags []uint8) {
	return n.clear, n.flags
}
