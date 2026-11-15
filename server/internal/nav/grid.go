// Package nav is a 3D map's walk grid: each cell's ground and clearance, moves by the sim's own rules, and A* for mobs.
package nav

import (
	"math"

	"finga/internal/sim"
)

// cell is the grid's spacing in metres.
const cell = 0.5

// Point is a position on the ground, in metres.
type Point struct{ X, Z float64 }

// Grid is a map's walk grid; mapfile builds a map's at its first use.
type Grid struct {
	nx, nz int // cells along x and z
	g      sim.Geometry
	step   float64
	ground []float64 // the highest of the terrain and any solid's top over the cell's centre
	onTop  []bool    // the ground there is a solid's top, not the terrain
	raised []bool    // and that top is more than step above the terrain: a wall to anyone walking on the ground
	clear  []float64 // from the centre to the nearest wall: a solid more than step above this cell's ground, or the map's edge
	steepX []bool    // the terrain cell under the centre is steep along x, by the sim's marks
	steepZ []bool    // or along z
}

// New builds the grid over g; step is the sim's: a solid's top up to that far above the feet is ground, a higher one a wall.
func New(g sim.Geometry, step float64) *Grid {
	n := &Grid{nx: int(math.Ceil(g.SizeX / cell)), nz: int(math.Ceil(g.SizeZ / cell)), g: g, step: step}
	size := n.nx * n.nz
	n.ground, n.onTop, n.raised, n.clear = make([]float64, size), make([]bool, size), make([]bool, size), make([]float64, size)
	n.steepX, n.steepZ = make([]bool, size), make([]bool, size)
	for i := range size {
		c := n.centre(i)
		s := stand(g, step, c)
		n.ground[i], n.onTop[i], n.raised[i], n.clear[i] = s.ground, s.onTop, s.raised, s.clear
		n.steepX[i], n.steepZ[i] = g.Terrain.Steep(c.X, c.Z, false), g.Terrain.Steep(c.X, c.Z, true)
	}
	return n
}

// spot is what a body standing at a point has under and around it.
type spot struct {
	ground        float64 // the highest of the terrain and any solid's top over the point
	onTop, raised bool    // the ground is a solid's top, and more than step above the terrain
	clear         float64 // from the point to the nearest wall to feet on that ground
}

func stand(g sim.Geometry, step float64, p Point) spot {
	terrain := g.Terrain.Height(p.X, p.Z)
	s := spot{ground: terrain}
	for _, b := range g.Boxes {
		if b.Top > s.ground && b.Holds(p.X, p.Z) {
			s.ground = b.Top
		}
	}
	for _, cy := range g.Cylinders {
		if cy.Top > s.ground && cy.Holds(p.X, p.Z) {
			s.ground = cy.Top
		}
	}
	s.onTop, s.raised = s.ground > terrain, s.ground > terrain+step
	s.clear = clearance(g, step, p, s.ground)
	return s
}

// clearance is how far p is from the nearest wall to feet at height y: a solid more than step above them, or the map's edge.
func clearance(g sim.Geometry, step float64, p Point, y float64) float64 {
	d := min(p.X, g.SizeX-p.X, p.Z, g.SizeZ-p.Z)
	for _, b := range g.Boxes {
		if b.Top > y+step {
			d = min(d, boxDistance(b, p))
		}
	}
	for _, cy := range g.Cylinders {
		if cy.Top > y+step {
			d = min(d, max(Distance(p, Point{X: cy.X, Z: cy.Z})-cy.Radius, 0))
		}
	}
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
	return Point{X: (float64(i%n.nx) + 0.5) * cell, Z: (float64(i/n.nx) + 0.5) * cell}
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
	return ok && !n.raised[i] && n.clear[i] >= r
}

// walk reports whether a body of radius r may walk from a to side neighbour b: not onto a solid over step up, nor uphill into a steep cell.
// As in the sim, the steep rule reads the terrain under b, so a solid's top on a slope rising past the feet is no way on.
func (n *Grid) walk(a, b int, r float64) bool {
	if n.clear[b] < r {
		return false
	}
	steep := n.steepX[b]
	if b-a == n.nx || a-b == n.nx {
		steep = n.steepZ[b]
	}
	if n.onTop[b] {
		c := n.centre(b)
		return n.ground[b]-n.ground[a] <= n.step && !(steep && n.g.Terrain.Height(c.X, c.Z) > n.ground[a])
	}
	return n.ground[b] <= n.ground[a] || !steep
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
