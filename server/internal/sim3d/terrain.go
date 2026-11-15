package sim3d

import "math"

// Terrain is the ground's height grid, Cell metres apart, with each cell's steep marks; NewTerrain builds it.
type Terrain struct {
	Cell    float64   `json:"cell"`
	NX      int       `json:"nx"`      // samples along x
	NZ      int       `json:"nz"`      // samples along z
	Heights []float64 `json:"heights"` // row by row from z = 0, each row from x = 0
	steepX  []bool    // per cell: a rise across either of its x edges exceeds MaxSlope × Cell
	steepZ  []bool    // likewise across its z edges
}

// NewTerrain is the grid with its steep marks for maxSlope, a rise per metre.
func NewTerrain(cell float64, nx, nz int, heights []float64, maxSlope float64) Terrain {
	t := Terrain{Cell: cell, NX: nx, NZ: nz, Heights: heights}
	limit := float64(maxSlope * cell)
	t.steepX = make([]bool, (nx-1)*(nz-1))
	t.steepZ = make([]bool, (nx-1)*(nz-1))
	for iz := range nz - 1 {
		for ix := range nx - 1 {
			h00, h10 := heights[iz*nx+ix], heights[iz*nx+ix+1]
			h01, h11 := heights[(iz+1)*nx+ix], heights[(iz+1)*nx+ix+1]
			c := iz*(nx-1) + ix
			t.steepX[c] = math.Abs(h10-h00) > limit || math.Abs(h11-h01) > limit
			t.steepZ[c] = math.Abs(h01-h00) > limit || math.Abs(h11-h10) > limit
		}
	}
	return t
}

// at is the cell holding coordinate v along an axis of n samples, and how far into it v lies, 0 to 1.
func (t Terrain) at(v float64, n int) (int, float64) {
	f := v / t.Cell
	i := int(math.Floor(f))
	switch {
	case i < 0:
		i = 0
	case i > n-2:
		i = n - 2
	}
	return i, f - float64(i)
}

// height is the bilinear height at (x, z).
func (t Terrain) height(x, z float64) float64 {
	ix, fx := t.at(x, t.NX)
	iz, fz := t.at(z, t.NZ)
	h00, h10 := t.Heights[iz*t.NX+ix], t.Heights[iz*t.NX+ix+1]
	h01, h11 := t.Heights[(iz+1)*t.NX+ix], t.Heights[(iz+1)*t.NX+ix+1]
	near := h00 + float64((h10-h00)*fx)
	far := h01 + float64((h11-h01)*fx)
	return near + float64((far-near)*fz)
}

// top is the highest of the four samples of the cell holding (x, z).
func (t Terrain) top(x, z float64) float64 {
	ix, _ := t.at(x, t.NX)
	iz, _ := t.at(z, t.NZ)
	i := iz*t.NX + ix
	return max(t.Heights[i], t.Heights[i+1], t.Heights[i+t.NX], t.Heights[i+t.NX+1])
}

// steep reports whether the cell holding (x, z) is steep along x, or along z when alongZ.
func (t Terrain) steep(x, z float64, alongZ bool) bool {
	ix, _ := t.at(x, t.NX)
	iz, _ := t.at(z, t.NZ)
	if alongZ {
		return t.steepZ[iz*(t.NX-1)+ix]
	}
	return t.steepX[iz*(t.NX-1)+ix]
}
