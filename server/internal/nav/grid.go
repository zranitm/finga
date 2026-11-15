// Package nav is the walk grid over a 3D map: each cell's ground and clearance, the moves between neighbours by the sim's own rules,
// and A* for mobs. The reach check's flood, which also jumps, is in its tests: it runs on the shipped maps once, not on every load.
package nav

import (
	"math"

	"finga/internal/sim3d"
)

// Cell is the grid's spacing in metres.
const Cell = 0.5

// Point is a position on the ground, in metres.
type Point struct{ X, Z float64 }

// Grid is a map's walk grid, built once at load.
type Grid struct {
	NX, NZ int // cells along x and z
	g      sim3d.Geometry
	step   float64
	ground []float64 // the highest of the terrain and any solid's top over the cell's centre
	onTop  []bool    // the ground there is a solid's top, not the terrain
	raised []bool    // and that top is more than step above the terrain: a wall to anyone walking on the ground
	clear  []float64 // from the centre to the nearest wall: a solid more than step above this cell's ground, or the map's edge
	steepX []bool    // the terrain cell under the centre is steep along x, by the sim's marks
	steepZ []bool    // or along z
}

// New builds the grid over g; step is the sim's: a solid's top up to that far above the feet is ground, a higher one a wall.
func New(g sim3d.Geometry, step float64) *Grid {
	n := &Grid{NX: int(math.Ceil(g.SizeX / Cell)), NZ: int(math.Ceil(g.SizeZ / Cell)), g: g, step: step}
	size := n.NX * n.NZ
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

func stand(g sim3d.Geometry, step float64, p Point) spot {
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

// clearance is how far p is from the nearest wall to feet at height y: a solid whose top is more than step above them, or the
// map's edge.
func clearance(g sim3d.Geometry, step float64, p Point, y float64) float64 {
	d := min(p.X, g.SizeX-p.X, p.Z, g.SizeZ-p.Z)
	for _, b := range g.Boxes {
		if b.Top > y+step {
			d = min(d, boxDistance(b, p))
		}
	}
	for _, cy := range g.Cylinders {
		if cy.Top > y+step {
			d = min(d, max(math.Hypot(p.X-cy.X, p.Z-cy.Z)-cy.Radius, 0))
		}
	}
	return d
}

// FitsAt reports whether a body of radius r can stand at p on g's ground, as the grid decides it for a cell's centre: inside the map,
// not up on a solid a walker can't step onto, and clear of every wall. It needs no grid, so the loader checks a map's spots with it.
func FitsAt(g sim3d.Geometry, step float64, p Point, r float64) bool {
	if p.X < 0 || p.Z < 0 || p.X > g.SizeX || p.Z > g.SizeZ {
		return false
	}
	s := stand(g, step, p)
	return !s.raised && s.clear >= r
}

// boxDistance is how far p lies outside b's footprint, 0 inside it.
func boxDistance(b sim3d.Box, p Point) float64 {
	dx, dz := p.X-b.X, p.Z-b.Z
	u, v := dx*b.UX+dz*b.UZ, dz*b.UX-dx*b.UZ
	return math.Hypot(max(math.Abs(u)-b.HalfW, 0), max(math.Abs(v)-b.HalfD, 0))
}

func (n *Grid) centre(i int) Point {
	return Point{X: (float64(i%n.NX) + 0.5) * Cell, Z: (float64(i/n.NX) + 0.5) * Cell}
}

// index is the cell holding p, false outside the map.
func (n *Grid) index(p Point) (int, bool) {
	ix, iz := int(math.Floor(p.X/Cell)), int(math.Floor(p.Z/Cell))
	if ix < 0 || iz < 0 || ix >= n.NX || iz >= n.NZ {
		return 0, false
	}
	return iz*n.NX + ix, true
}

// Fits reports whether a body of radius r can stand at p on the ground: inside the map, not up on a solid a walker can't step
// onto, and clear of every wall.
func (n *Grid) Fits(p Point, r float64) bool {
	i, ok := n.index(p)
	return ok && !n.raised[i] && n.clear[i] >= r
}

// Ground is the height a body standing at p stands at.
func (n *Grid) Ground(p Point) float64 {
	i, _ := n.index(p)
	return n.ground[i]
}

// walk reports whether a body of radius r may walk from cell a to its side neighbour b, by the sim's rules: never onto a solid's top
// more than step up, never uphill into a terrain cell steep along the way it moves.
func (n *Grid) walk(a, b int, r float64) bool {
	if n.clear[b] < r {
		return false
	}
	rise := n.ground[b] - n.ground[a]
	if n.onTop[b] {
		return rise <= n.step
	}
	if b-a == n.NX || a-b == n.NX {
		return rise <= 0 || !n.steepZ[b]
	}
	return rise <= 0 || !n.steepX[b]
}

// sides are a cell's four side neighbours inside the grid, west, east, south and north; -1 where there's none.
func (n *Grid) sides(i int) [4]int {
	x, z := i%n.NX, i/n.NX
	out := [4]int{-1, -1, -1, -1}
	if x > 0 {
		out[0] = i - 1
	}
	if x < n.NX-1 {
		out[1] = i + 1
	}
	if z > 0 {
		out[2] = i - n.NX
	}
	if z < n.NZ-1 {
		out[3] = i + n.NX
	}
	return out
}

// moves calls visit with each neighbour a body of radius r may walk to from i, side or diagonal; a diagonal needs both of its
// two-move routes walkable, as the sim moves x then z and the body mustn't cut a wall's corner.
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
