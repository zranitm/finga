package regiongen

import (
	"fmt"
	"math"
)

// AreaCell is the side in metres of the squares a region's areas are judged on, each held by the area holding its south-west corner (large-regions.md section 6).
const AreaCell = 4

// AreaGrid is which area holds each AreaCell square, row by row from the south-west: an index into the plan's areas, or -1 for none.
type AreaGrid struct {
	NX, NZ int
	Of     []int16
}

// Holding is the area holding (x, z): its square's, held to the region.
func (a AreaGrid) Holding(x, z float64) int {
	ix := min(max(int(math.Floor(x/AreaCell)), 0), a.NX-1)
	iz := min(max(int(math.Floor(z/AreaCell)), 0), a.NZ-1)
	return int(a.Of[iz*a.NX+ix])
}

// AreaGrid lays the plan's areas on AreaCell squares, a corner in a polygon by the rule the generator samples by; two areas holding one square is an error.
func (p Plan) AreaGrid() (AreaGrid, error) {
	g := grid{nx: int(p.Size[0] / AreaCell), nz: int(p.Size[1] / AreaCell), cell: AreaCell}
	out := AreaGrid{NX: g.nx, NZ: g.nz, Of: make([]int16, g.nx*g.nz)}
	for s := range out.Of {
		out.Of[s] = -1
	}
	var err error
	for i, a := range p.Areas {
		g.fill(a.Area, func(s int) {
			if o := out.Of[s]; o >= 0 && err == nil {
				err = fmt.Errorf("areas %s and %s both hold the %d m square at [%g %g]", p.Areas[o].ID, a.ID, AreaCell, g.x(s%g.nx), g.x(s/g.nx))
			}
			out.Of[s] = int16(i) //nolint:gosec // a plan has far fewer than 2¹⁵ areas
		})
	}
	return out, err
}
