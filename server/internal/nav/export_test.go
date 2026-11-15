package nav

import "math"

// GroundAt is the ground of p's cell, NaN off the map.
func (n *Grid) GroundAt(p Point) float64 {
	if i, ok := n.index(p); ok {
		return n.ground[i]
	}
	return math.NaN()
}

// Cells are every cell's ground and clearance, which a spot's verdict rests on.
func (n *Grid) Cells() (ground, clear []float64) { return n.ground, n.clear }
