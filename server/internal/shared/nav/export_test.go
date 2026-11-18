package nav

import (
	"math"
	"sync"
)

// MaxClear is the most clearance a cell holds.
const MaxClear = maxClear

// GroundAt is the ground of p's cell, NaN off the map.
func (n *Grid) GroundAt(p Point) float64 {
	if i, ok := n.index(p); ok {
		return n.spot(i).ground
	}
	return math.NaN()
}

var (
	spotsMu sync.Mutex
	spotsOf = map[*Grid][]spot{}
)

// spot is cell i's ground and clearance, as the build found them; a grid's are worked out once, on first asking.
func (n *Grid) spot(i int) spot {
	spotsMu.Lock()
	defer spotsMu.Unlock()
	all, ok := spotsOf[n]
	if !ok {
		all = make([]spot, n.nx*n.nz)
		for k := range all {
			all[k] = stand(n.g, n.step, n.centre(k))
		}
		spotsOf[n] = all
	}
	return all[i]
}

// Cells are every cell's clearance and flags, which a spot's verdict and every walk rest on.
func (n *Grid) Cells() (clear []uint16, flags []uint8) {
	return n.clear, n.flags
}
