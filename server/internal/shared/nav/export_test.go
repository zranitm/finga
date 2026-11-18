package nav

import "math"

// GroundAt is the ground of p's cell, NaN off the map.
func (n *Grid) GroundAt(p Point) float64 {
	if i, ok := n.index(p); ok {
		return float64(n.ground[i])
	}
	return math.NaN()
}

// Cells are every cell's ground, clearance and flags, which a spot's verdict rests on.
func (n *Grid) Cells() (ground []float32, clear []uint16, flags []uint8) {
	return n.ground, n.clear, n.flags
}
