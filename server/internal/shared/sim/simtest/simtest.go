// Package simtest draws random maps for property tests of the sim and what is built on it.
package simtest

import (
	"math"

	"pgregory.net/rapid"

	"finga/internal/shared/sim"
)

// bucketM is the sim's bucket side, whose lines the solids are drawn across.
const bucketM = 8.0

// Geometry draws a map 2 to 120 m a side with rolling terrain, and boxes and cylinders of every size: turned any way, small or spanning many buckets, low enough to step onto or tall, some past the map's edges.
func Geometry(t *rapid.T, maxSlope float64) sim.Geometry {
	cell := rapid.SampledFrom([]float64{0.5, 1, 2, 4}).Draw(t, "cell")
	nx := rapid.IntRange(2, int(120/cell)).Draw(t, "nx")
	nz := rapid.IntRange(2, int(120/cell)).Draw(t, "nz")
	amp, fx, fz := rapid.Float64Range(0, 1.5).Draw(t, "amp"), rapid.Float64Range(0, 0.6).Draw(t, "fx"), rapid.Float64Range(0, 0.6).Draw(t, "fz")
	heights := make([]float64, 0, (nx+1)*(nz+1))
	for iz := range nz + 1 {
		for ix := range nx + 1 {
			heights = append(heights, amp*math.Sin(float64(ix)*cell*fx)*math.Cos(float64(iz)*cell*fz))
		}
	}
	g := sim.Geometry{
		SizeX: float64(nx) * cell, SizeZ: float64(nz) * cell,
		Terrain:   sim.NewTerrain(cell, nx+1, nz+1, heights, maxSlope),
		Boxes:     []sim.Box{},
		Cylinders: []sim.Cylinder{},
	}
	size := func(label string) float64 { // mostly small, some a bucket or several across
		return rapid.Float64Range(0.05, 1.2).Draw(t, label) * rapid.SampledFrom([]float64{1, 1, 1, 4, 20}).Draw(t, label+" scale")
	}
	// at is an x or z on a side of size: anywhere, or with a solid's edge, reach from its centre, just across a bucket's edge from a body.
	at := func(side, reach float64, label string) float64 {
		if !rapid.Bool().Draw(t, label+" by a bucket's edge") {
			return rapid.Float64Range(-4, side+4).Draw(t, label)
		}
		line := bucketM * float64(rapid.IntRange(0, int(side/bucketM)+1).Draw(t, label+" line"))
		way := rapid.SampledFrom([]float64{-1, 1}).Draw(t, label+" side")
		return line + way*(reach+rapid.Float64Range(0, 0.15).Draw(t, label+" gap"))
	}
	top := rapid.Float64Range(-1.5, 4)
	for range rapid.IntRange(0, 40).Draw(t, "boxes") {
		w, d := size("half w"), size("half d")
		turn := 0.0 // square to the buckets, its edges along their lines
		if rapid.Bool().Draw(t, "turned") {
			turn = rapid.Float64Range(0, 2*math.Pi).Draw(t, "turn")
		}
		g.Boxes = append(g.Boxes, sim.Box{X: at(g.SizeX, w, "box x"), Z: at(g.SizeZ, d, "box z"), HalfW: w, HalfD: d,
			UX: math.Cos(turn), UZ: math.Sin(turn), Top: top.Draw(t, "box top")})
	}
	for range rapid.IntRange(0, 40).Draw(t, "cylinders") {
		r := size("radius")
		g.Cylinders = append(g.Cylinders, sim.Cylinder{X: at(g.SizeX, r, "cylinder x"), Z: at(g.SizeZ, r, "cylinder z"), Radius: r, Top: top.Draw(t, "cylinder top")})
	}
	for range rapid.IntRange(0, 4).Draw(t, "blockers") {
		r, bottom := size("blocker radius"), top.Draw(t, "blocker bottom")
		g.Blockers = append(g.Blockers, sim.Blocker{X: at(g.SizeX, r, "blocker x"), Z: at(g.SizeZ, r, "blocker z"), Radius: r, Bottom: bottom, Top: bottom + 2})
	}
	return g
}

// Near draws a point of g, half the time beside one of its solids, where moves catch on them.
func Near(t *rapid.T, g sim.Geometry) (x, z float64) {
	n := len(g.Boxes) + len(g.Cylinders)
	if n == 0 || rapid.Bool().Draw(t, "anywhere") {
		return rapid.Float64Range(0, g.SizeX).Draw(t, "x"), rapid.Float64Range(0, g.SizeZ).Draw(t, "z")
	}
	k := rapid.IntRange(0, n-1).Draw(t, "solid")
	var cx, cz, reach float64
	if k < len(g.Boxes) {
		b := g.Boxes[k]
		cx, cz, reach = b.X, b.Z, math.Hypot(b.HalfW, b.HalfD)
	} else {
		c := g.Cylinders[k-len(g.Boxes)]
		cx, cz, reach = c.X, c.Z, c.Radius
	}
	turn, d := rapid.Float64Range(0, 2*math.Pi).Draw(t, "around"), rapid.Float64Range(0, reach+1).Draw(t, "out")
	return min(max(cx+d*math.Cos(turn), 0), g.SizeX), min(max(cz+d*math.Sin(turn), 0), g.SizeZ)
}
