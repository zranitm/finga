package maps3d

import (
	"errors"
	"fmt"
	"math"

	"finga/internal/nav"
	"finga/internal/sim3d"
)

// Map is a map file built for the sim and the walk grid.
type Map struct {
	ID string
	File
	Geometry sim3d.Geometry
	Nav      *nav.Grid
}

// Point is a file position as the walk grid's.
func Point(at [2]float64) nav.Point { return nav.Point{X: at[0], Z: at[1]} }

// Build checks f's shapes and builds its terrain, solids and walk grid for the sim's params p; the loader checks the rest.
func Build(id string, f File, p sim3d.Params) (*Map, error) {
	if !positive(f.Size[0]) || !positive(f.Size[1]) {
		return nil, fmt.Errorf("size %v: both sides must be positive", f.Size)
	}
	if f.Look.Ground == "" || f.Look.Sky == "" {
		return nil, errors.New("look needs a ground and a sky")
	}
	t, err := terrain(f.Terrain, f.Size, p.MaxSlope)
	if err != nil {
		return nil, fmt.Errorf("terrain: %w", err)
	}
	g := sim3d.Geometry{SizeX: f.Size[0], SizeZ: f.Size[1], Terrain: t, Boxes: []sim3d.Box{}, Cylinders: []sim3d.Cylinder{}}
	for i, s := range f.Solids {
		if err := solid(&g, f.Size, s); err != nil {
			return nil, fmt.Errorf("solid %d (%s): %w", i+1, s.Model, err)
		}
	}
	for i, pr := range f.Props {
		if err := prop(f.Size, pr); err != nil {
			return nil, fmt.Errorf("prop %d: %w", i+1, err)
		}
	}
	return &Map{ID: id, File: f, Geometry: g, Nav: nav.New(g, p.Step)}, nil
}

// terrain samples every hill and mesa at each grid point, in file order, so every build sums them alike.
func terrain(t Terrain, size [2]float64, maxSlope float64) (sim3d.Terrain, error) {
	nx, okX := samples(size[0], t.Cell)
	nz, okZ := samples(size[1], t.Cell)
	if !positive(t.Cell) || !okX || !okZ {
		return sim3d.Terrain{}, fmt.Errorf("cell %g must be positive and divide the size %g × %g", t.Cell, size[0], size[1])
	}
	for i, h := range t.Hills {
		if !positive(h.Radius) || !finite(h.Height) || !finite(h.At[0]) || !finite(h.At[1]) {
			return sim3d.Terrain{}, fmt.Errorf("hill %d: needs a positive radius and a height", i+1)
		}
	}
	for i, m := range t.Mesas {
		if !positive(m.Area[2]) || !positive(m.Area[3]) || !positive(m.Edge) || !finite(m.Height) || !finite(m.Area[0]) || !finite(m.Area[1]) {
			return sim3d.Terrain{}, fmt.Errorf("mesa %d: needs an area of positive width and depth, a height, and a positive edge", i+1)
		}
	}
	heights := make([]float64, nx*nz)
	for iz := range nz {
		for ix := range nx {
			x, z := float64(float64(ix)*t.Cell), float64(float64(iz)*t.Cell)
			h := 0.0
			for _, hl := range t.Hills {
				h += hill(hl, x, z)
			}
			for _, m := range t.Mesas {
				h += mesa(m, x, z)
			}
			heights[iz*nx+ix] = h
		}
	}
	return sim3d.NewTerrain(t.Cell, nx, nz, heights, maxSlope), nil
}

// samples is how many grid points span length at cell apart, and whether cell divides it.
func samples(length, cell float64) (int, bool) {
	k := math.Round(length / cell)
	return int(k) + 1, k >= 1 && math.Abs(float64(k*cell)-length) < 1e-9
}

// hill is Height × (1 − d²/r²)², with no sqrt, smooth at its top and its foot.
func hill(h Hill, x, z float64) float64 {
	dx, dz := x-h.At[0], z-h.At[1]
	d2, r2 := float64(dx*dx)+float64(dz*dz), float64(h.Radius*h.Radius)
	if d2 >= r2 {
		return 0
	}
	w := 1 - d2/r2
	return float64(h.Height * float64(w*w))
}

// mesa is Height over its area, falling straight to 0 across Edge outside it, its corners rounded.
func mesa(m Mesa, x, z float64) float64 {
	ex := max(m.Area[0]-x, x-(m.Area[0]+m.Area[2]), 0)
	ez := max(m.Area[1]-z, z-(m.Area[1]+m.Area[3]), 0)
	d := math.Sqrt(float64(ex*ex) + float64(ez*ez))
	if d >= m.Edge {
		return 0
	}
	return float64(m.Height * (1 - d/m.Edge))
}

func solid(g *sim3d.Geometry, size [2]float64, s Solid) error {
	if s.Model == "" {
		return errors.New("needs a model")
	}
	if !positive(s.Height) {
		return errors.New("needs a positive height")
	}
	switch {
	case s.Box != nil && s.Cylinder == nil:
		b := *s.Box
		if !inside(size, b[0], b[1]) || !positive(b[2]) || !positive(b[3]) || s.Radius != 0 || !finite(s.Yaw) {
			return errors.New("a box needs its centre inside the map, a positive width and depth, and no radius")
		}
		sin, cos := sincos(s.Yaw)
		g.Boxes = append(g.Boxes, sim3d.Box{X: b[0], Z: b[1], HalfW: b[2] / 2, HalfD: b[3] / 2, UX: cos, UZ: unsigned(-sin), Top: g.Terrain.Height(b[0], b[1]) + s.Height})
	case s.Cylinder != nil && s.Box == nil:
		c := *s.Cylinder
		if !inside(size, c[0], c[1]) || !positive(s.Radius) || s.Yaw != 0 {
			return errors.New("a cylinder needs its centre inside the map, a positive radius, and no yaw")
		}
		g.Cylinders = append(g.Cylinders, sim3d.Cylinder{X: c[0], Z: c[1], Radius: s.Radius, Top: g.Terrain.Height(c[0], c[1]) + s.Height})
	default:
		return errors.New("is either a box or a cylinder")
	}
	return nil
}

func prop(size [2]float64, p Prop) error {
	switch {
	case p.At != nil && p.Scatter == nil:
		if p.Model == "" || !inside(size, p.At[0], p.At[1]) {
			return errors.New("needs a model and a spot inside the map")
		}
	case p.Scatter != nil && p.At == nil && p.Model == "":
		s := p.Scatter
		if s.Model == "" || s.Count < 1 || !positive(s.Area[2]) || !positive(s.Area[3]) || !inside(size, s.Area[0], s.Area[1]) ||
			!inside(size, s.Area[0]+s.Area[2], s.Area[1]+s.Area[3]) {
			return errors.New("a scatter needs a model, a count, and an area inside the map")
		}
	default:
		return errors.New("is either a model at a spot or a scatter")
	}
	return nil
}

// inside is whether (x, z) lies on the map, its edges included.
func inside(size [2]float64, x, z float64) bool {
	return x >= 0 && x <= size[0] && z >= 0 && z <= size[1]
}

// YAML can spell NaN and infinity, which pass a check like x <= 0; these refuse them.
func positive(x float64) bool { return x > 0 && !math.IsInf(x, 1) }

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
