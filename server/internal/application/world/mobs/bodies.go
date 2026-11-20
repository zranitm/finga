package mobs

import (
	"cmp"
	"math"
	"slices"

	"finga/internal/shared/gamedata"
)

// bodyPad widens a query past any rounding in the distances it stands in for.
const bodyPad = 1e-6

// cells are a map's interest cells (large-regions.md section 5), the squares its bodies are bucketed in by their centres.
type cells struct {
	per    float64 // cells a metre
	nx, nz int
}

func newCells(in gamedata.Interest, sizeX, sizeZ float64) cells {
	nx, nz := in.Cells(sizeX, sizeZ)
	return cells{per: 1 / in.Cell, nx: nx, nz: nz}
}

// of is the cell holding (x, z), held to the map: past an edge, the edge's.
func (c cells) of(x, z float64) int { return c.along(z, c.nz)*c.nx + c.along(x, c.nx) }

func (c cells) along(v float64, n int) int {
	f := math.Floor(v * c.per)
	switch {
	case f < 0 || math.IsNaN(f): // NaN, which no body's distance test passes
		return 0
	case f >= float64(n):
		return n - 1
	}
	return int(f)
}

// near walks lists, one a cell, of the cells within reach of the box [x0, x1] by [z0, z1], merged in key order; all in full when they're more than a walk merges.
func near[T any](c cells, in [][]T, all []T, key func(T) uint64, x0, z0, x1, z1, reach float64) walk[T] {
	reach += bodyPad
	ix0, ix1 := c.along(x0-reach, c.nx), c.along(x1+reach, c.nx)
	iz0, iz1 := c.along(z0-reach, c.nz), c.along(z1+reach, c.nz)
	w := walk[T]{key: key}
	if (ix1-ix0+1)*(iz1-iz0+1) > len(w.lists) {
		w.lists[0], w.n = all, 1
		return w
	}
	for iz := iz0; iz <= iz1; iz++ {
		for ix := ix0; ix <= ix1; ix++ {
			if l := in[iz*c.nx+ix]; len(l) > 0 {
				w.lists[w.n] = l
				w.n++
			}
		}
	}
	return w
}

// walk goes through some cells' lists as one, smallest key first; a body lies in one cell, so none comes twice.
type walk[T any] struct {
	lists [16][]T
	at    [16]int
	n     int
	key   func(T) uint64
}

func (w *walk[T]) next() (T, bool) {
	best := -1
	for k := range w.n {
		if w.at[k] < len(w.lists[k]) && (best < 0 || w.key(w.lists[k][w.at[k]]) < w.key(w.lists[best][w.at[best]])) {
			best = k
		}
	}
	if best < 0 {
		var none T
		return none, false
	}
	w.at[best]++
	return w.lists[best][w.at[best]-1], true
}

// playerCells are the tick's living players by the cell under their centres, each cell's indices ascending.
type playerCells struct {
	cells
	in    [][]int32
	all   []int32
	reach float64 // the widest player's radius
}

func playerIndex(i int32) uint64 { return uint64(i) } //nolint:gosec // an index is never negative

// set buckets players, the tick's, reusing g's lists.
func (g *playerCells) set(players []Body) {
	for k := range g.in {
		g.in[k] = g.in[k][:0]
	}
	g.all, g.reach = g.all[:0], 0
	for i, p := range players {
		k := g.of(p.X, p.Z)
		g.in[k] = append(g.in[k], int32(i)) //nolint:gosec // a map's players are far fewer than 2³¹
		g.all = append(g.all, int32(i))     //nolint:gosec // as above
		g.reach = max(g.reach, p.Radius)
	}
}

// mobCells are the living mobs by the cell under their centres, each cell's by id; a mob moving into another cell moves there.
type mobCells struct {
	cells
	in    [][]*mob
	reach float64 // the widest mob's radius yet
}

func mobID(mb *mob) uint64 { return mb.id }

func (g *mobCells) add(mb *mob) {
	mb.cell = g.of(mb.x, mb.z)
	l := g.in[mb.cell]
	k, _ := slices.BinarySearchFunc(l, mb.id, func(o *mob, id uint64) int { return cmp.Compare(o.id, id) })
	g.in[mb.cell] = slices.Insert(l, k, mb)
	g.reach = max(g.reach, mb.def.Radius)
}

func (g *mobCells) remove(mb *mob) {
	g.in[mb.cell] = slices.DeleteFunc(g.in[mb.cell], func(o *mob) bool { return o == mb })
}

// moved files mb under the cell it stands in now.
func (g *mobCells) moved(mb *mob) {
	if g.of(mb.x, mb.z) != mb.cell {
		g.remove(mb)
		g.add(mb)
	}
}

// playersNear walks the tick's players whose centres may lie within reach of the box [x0, x1] by [z0, z1], in their order.
func (a *Arena) playersNear(x0, z0, x1, z1, reach float64) walk[int32] {
	return near(a.near.cells, a.near.in, a.near.all, playerIndex, x0, z0, x1, z1, reach)
}

// mobsNear walks the living mobs whose centres may lie within reach of the box [x0, x1] by [z0, z1], in id order, which is a.mobs's.
func (a *Arena) mobsNear(x0, z0, x1, z1, reach float64) walk[*mob] {
	return near(a.bodies.cells, a.bodies.in, a.mobs, mobID, x0, z0, x1, z1, reach)
}
