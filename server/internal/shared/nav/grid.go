// Package nav is a 3D map's walk grid: each cell's ground and clearance, moves by the sim's own rules, and A* for mobs.
package nav

import (
	"fmt"
	"math"

	"finga/internal/shared/sim"
)

// cell is the grid's spacing in metres.
const cell = 0.5

// Point is a position on the ground, in metres.
type Point struct{ X, Z float64 }

// clearUnit is a clearance's step, 2⁻¹³ m: scaling by a power of two is exact, so a radius in whole steps fits as a float clearance would.
const clearUnit = 1.0 / 8192

// maxClear is the most clearance a cell holds, just under 8 m; a radius past it never fits.
const maxClear = math.MaxUint16 * clearUnit

// A cell's flags.
const (
	raised  uint8 = 1 << iota // its ground is a solid's top more than step above the terrain: a wall to anyone walking on the ground
	toWest                    // the way to the west neighbour is open, to a body that fits there
	toEast                    // east
	toSouth                   // south
	toNorth                   // north
)

// What a cell's ground is, needed only while the grid is built.
const (
	onTop  uint8 = 1 << iota // the ground is a solid's top, not the terrain
	steepX                   // the terrain cell under the centre is steep along x, by the sim's marks
	steepZ                   // or along z
)

// Grid is a map's walk grid, 3 bytes a cell; the loader builds a map's at boot.
type Grid struct {
	nx, nz int // cells along x and z
	g      sim.Geometry
	step   float64
	clear  []uint16 // from the centre to the nearest wall, in clearUnits up to maxClear: a solid more than step above this cell's ground, or the map's edge
	flags  []uint8
}

// New builds the grid over g, each cell reading only the solids near it when g is sim.Indexed; step is the sim's: a solid's top up to that far above the feet is ground, a higher one a wall. Which ways are open is decided here from the ground in full, so the grid keeps no heights.
func New(g sim.Geometry, step float64) *Grid {
	n := &Grid{nx: int(math.Ceil(g.SizeX / cell)), nz: int(math.Ceil(g.SizeZ / cell)), g: g, step: step}
	size := n.nx * n.nz
	n.clear, n.flags = make([]uint16, size), make([]uint8, size)
	ground, marks := make([]float64, size), make([]uint8, size)
	for i := range size {
		c := n.centre(i)
		s := stand(g, step, c)
		ground[i], n.clear[i] = s.ground, uint16(math.Floor(s.clear/clearUnit)) // s.clear is at most maxClear
		n.flags[i] = flag(s.raised, raised)
		marks[i] = flag(s.onTop, onTop) | flag(g.Terrain.Steep(c.X, c.Z, false), steepX) | flag(g.Terrain.Steep(c.X, c.Z, true), steepZ)
	}
	for i := range size {
		for k, j := range n.sides(i) {
			if j >= 0 && n.open(ground, marks, i, j) {
				n.flags[i] |= toWest << k
			}
		}
	}
	return n
}

func flag(on bool, f uint8) uint8 {
	if on {
		return f
	}
	return 0
}

// spot is what a body standing at a point has under and around it.
type spot struct {
	ground        float64 // the highest of the terrain and any solid's top over the point
	onTop, raised bool    // the ground is a solid's top, and more than step above the terrain
	clear         float64 // from the point to the nearest wall to feet on that ground, up to maxClear
}

func stand(g sim.Geometry, step float64, p Point) spot {
	terrain := g.Terrain.Height(p.X, p.Z)
	s := spot{ground: sim.Ground(p.X, p.Z, math.Inf(1), g)}
	s.onTop, s.raised = s.ground > terrain, s.ground > terrain+step
	s.clear = clearance(g, step, p, s.ground)
	return s
}

// clearance is how far p is from the nearest wall to feet at height y, up to maxClear: a solid more than step above them, or the map's edge. It reads only the solids within that reach.
func clearance(g sim.Geometry, step float64, p Point, y float64) float64 {
	d := min(p.X, g.SizeX-p.X, p.Z, g.SizeZ-p.Z, maxClear)
	reach := d + 1e-6
	g.Near(p.X-reach, p.Z-reach, p.X+reach, p.Z+reach, func(b sim.Box) {
		if b.Top > y+step {
			d = min(d, boxDistance(b, p))
		}
	}, func(cy sim.Cylinder) {
		if cy.Top > y+step {
			d = min(d, max(Distance(p, Point{X: cy.X, Z: cy.Z})-cy.Radius, 0))
		}
	})
	return d
}

// FitsAt is Fits at any point of g without building a grid, so the loader checks a map's spots by the grid's own rule.
func FitsAt(g sim.Geometry, step float64, p Point, r float64) bool {
	if p.X < 0 || p.Z < 0 || p.X > g.SizeX || p.Z > g.SizeZ {
		return false
	}
	s := stand(g, step, p)
	return !s.raised && s.clear >= r
}

// boxDistance is how far p lies outside b's footprint, 0 inside it.
func boxDistance(b sim.Box, p Point) float64 {
	dx, dz := p.X-b.X, p.Z-b.Z
	u, v := float64(dx*b.UX)+float64(dz*b.UZ), float64(dz*b.UX)-float64(dx*b.UZ)
	return Length(max(math.Abs(u)-b.HalfW, 0), max(math.Abs(v)-b.HalfD, 0))
}

// Distance is how far apart a and b are, the same bits on every CPU: each product is rounded, so arm64 can't fuse it into an add.
func Distance(a, b Point) float64 { return Length(a.X-b.X, a.Z-b.Z) }

// Length is the length of (x, z), the same bits on every CPU as Distance.
func Length(x, z float64) float64 { return math.Sqrt(float64(x*x) + float64(z*z)) }

func (n *Grid) centre(i int) Point {
	return Point{X: float64((float64(i%n.nx) + 0.5) * cell), Z: float64((float64(i/n.nx) + 0.5) * cell)}
}

// index is the cell holding p, false outside the map.
func (n *Grid) index(p Point) (int, bool) {
	ix, iz := int(math.Floor(p.X/cell)), int(math.Floor(p.Z/cell))
	if ix < 0 || iz < 0 || ix >= n.nx || iz >= n.nz {
		return 0, false
	}
	return iz*n.nx + ix, true
}

// Fits reports whether a body of radius r can stand at p: inside the map, not up on a solid too tall to step onto, clear of walls.
func (n *Grid) Fits(p Point, r float64) bool {
	i, ok := n.index(p)
	return ok && n.flags[i]&raised == 0 && n.fits(i, r)
}

// CheckRadius is nil when the grid judges a body of radius r as its float clearance would: a whole number of 2⁻¹³ m steps, as every 1/32 m is, and within maxClear. Off a step, a fit is judged for r rounded up to the next; past maxClear the body never fits.
func CheckRadius(r float64) error {
	switch {
	case !(r >= 0 && r <= maxClear):
		return fmt.Errorf("radius %v m is outside the walk grid's 0 to %v m", r, maxClear)
	case r/clearUnit != math.Trunc(r/clearUnit):
		return fmt.Errorf("radius %v m isn't a whole number of the walk grid's 2⁻¹³ m steps; a multiple of 1/32 m is", r)
	}
	return nil
}

// fits is whether cell i's clearance takes a body of radius r.
func (n *Grid) fits(i int, r float64) bool { return float64(n.clear[i]) >= r/clearUnit }

// walk reports whether a body of radius r may walk from a to side neighbour b: it fits at b, and the way there is open.
func (n *Grid) walk(a, b int, r float64) bool {
	return n.fits(b, r) && n.flags[a]&n.toward(a, b) != 0
}

// toward is the flag of the side b lies on from a.
func (n *Grid) toward(a, b int) uint8 {
	switch b - a {
	case n.nx:
		return toNorth
	case -n.nx:
		return toSouth
	case 1:
		return toEast
	}
	return toWest
}

// open reports whether a body may walk from a to side neighbour b, whatever its size, on the cells' ground and marks: not onto a solid over step up, nor uphill into or out of a cell whose terrain is steep that way, as in the sim, whose small steps end inside a first.
func (n *Grid) open(ground []float64, marks []uint8, a, b int) bool {
	way := steepX
	if b-a == n.nx || a-b == n.nx {
		way = steepZ
	}
	steep, from := marks[b]&way != 0, marks[a]&way != 0
	ga, gb := ground[a], ground[b]
	if marks[b]&onTop != 0 {
		c := n.centre(b)
		return gb-ga <= n.step && !(steep && n.g.Terrain.Height(c.X, c.Z) > ga)
	}
	return gb <= ga || !steep && (!from || marks[a]&onTop != 0) // a body on a solid stands clear of the terrain under it
}

// sides are a cell's four side neighbours inside the grid, west, east, south and north; -1 where there's none.
func (n *Grid) sides(i int) [4]int {
	x, z := i%n.nx, i/n.nx
	out := [4]int{-1, -1, -1, -1}
	if x > 0 {
		out[0] = i - 1
	}
	if x < n.nx-1 {
		out[1] = i + 1
	}
	if z > 0 {
		out[2] = i - n.nx
	}
	if z < n.nz-1 {
		out[3] = i + n.nx
	}
	return out
}

// moves calls visit with each neighbour a body of radius r walks to from i; a diagonal needs both side routes, so it never cuts a corner.
func (n *Grid) moves(i int, r float64, visit func(j int, diagonal bool)) {
	s := n.sides(i)
	var ok [4]bool
	for k, j := range s {
		if ok[k] = j >= 0 && n.walk(i, j, r); ok[k] {
			visit(j, false)
		}
	}
	for kx := range 2 {
		for kz := 2; kz < 4; kz++ {
			if xs, zs := s[kx], s[kz]; ok[kx] && ok[kz] {
				if d := zs + (xs - i); n.walk(xs, d, r) && n.walk(zs, d, r) {
					visit(d, true)
				}
			}
		}
	}
}
