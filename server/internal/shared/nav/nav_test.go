package nav_test

import (
	"math"
	"runtime"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// params are movement.yaml's.
var params = sim.Params{Dt: 1.0 / 60, WalkSpeed: 5, JumpVelocity: 12.34, Gravity: 38, MaxFallSpeed: 22.5, Radius: 0.375, Height: 1.75, Step: 0.3, MaxSlope: 0.7, Snap: 0.5}

// rise is the reach check's jump limit at the fastest move speed, as the loader takes it.
var rise = nav.JumpRise(params.Faster(1.3)) - 0.1

// flat is a sizeX × sizeZ map of level ground with the given solids.
func flat(sizeX, sizeZ float64, boxes []sim.Box, cylinders []sim.Cylinder) sim.Geometry {
	return sim.Geometry{SizeX: sizeX, SizeZ: sizeZ, Terrain: sim.NewTerrain(sizeX, 2, 2, []float64{0, 0, 0, 0}, params.MaxSlope), Boxes: boxes, Cylinders: cylinders}
}

// rising is a 12 × 6 m map at cell 0.5 whose ground stands at profile(x) along x.
func rising(profile func(x float64) float64) sim.Geometry {
	const nx, nz = 25, 13
	h := make([]float64, nx*nz)
	for iz := range nz {
		for ix := range nx {
			h[iz*nx+ix] = profile(float64(ix) * 0.5)
		}
	}
	return sim.Geometry{SizeX: 12, SizeZ: 6, Terrain: sim.NewTerrain(0.5, nx, nz, h, params.MaxSlope), Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}}
}

// simGetsUp plays a player holding east from x = 3 for 5 s, jumping whenever they stand if jumping: do they ever stand on top?
func simGetsUp(g sim.Geometry, top float64, jumping bool) bool {
	s := sim.State{X: 3, Z: 3, OnGround: true, HX: 1}
	for range 300 {
		s = sim.Step(s, sim.Input{Right: true, Jump: jumping && s.OnGround}, g, params)
		if s.OnGround && s.X > 9 && s.Y == top {
			return true
		}
	}
	return false
}

// The sim, A* and the reach check read the same steep marks and solids, so they agree on walking up a slope or a step, and jumping.
func TestOneSetOfRules(t *testing.T) {
	slope := func(rise float64, from, to float64) sim.Geometry {
		return rising(func(x float64) float64 { return rise * min(max((x-from)/(to-from), 0), 1) })
	}
	block := func(top float64) sim.Geometry { // a box from x = 6 to the east edge
		g := rising(func(float64) float64 { return 0 })
		g.Boxes = []sim.Box{{X: 9, Z: 3, HalfW: 3, HalfD: 3, UX: 1, Top: top}}
		return g
	}
	sunk := slope(0.4, 6, 6.5) // a cliff within one cell, its middle under a box's top within step of the feet
	sunk.Boxes = []sim.Box{{X: 9, Z: 3, HalfW: 3, HalfD: 3, UX: 1, Top: 0.25}}
	tests := []struct {
		name          string
		g             sim.Geometry
		top           float64
		walks, climbs bool
	}{
		{"a ramp under max_slope", slope(1.2, 5, 9), 1.2, true, true},
		{"a cliff within one cell", slope(1.2, 6, 6.5), 1.2, false, true},
		{"a cliff straddling two cells", slope(1.2, 6, 7), 1.2, false, true},
		{"a cliff too high to jump", slope(2.5, 6, 6.5), 2.5, false, false},
		{"a box within step", block(0.25), 0.25, true, true},
		{"a box over step", block(1.2), 1.2, false, true},
		{"a box above the jump's rise, its top within step of the feet", block(2), 2, false, true},
		{"a box too high to jump", block(2.5), 2.5, false, false},
		{"a box's top on a cliff, the cliff rising past the feet", sunk, 0.4, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.g
			n := nav.New(g, params.Step)
			from, up := nav.Point{X: 3.25, Z: 3.25}, nav.Point{X: 10.25, Z: 3.25}
			walk := map[string]bool{"sim": simGetsUp(g, tt.top, false), "A*": new(nav.Finder).Path(n, from, up, params.Radius, from, 20) != nil}
			climb := map[string]bool{"sim": simGetsUp(g, tt.top, true), "reach": n.Reach([]nav.Point{from}, params.Radius, rise).Near(up, 0.1)}
			for by, got := range walk {
				if got != tt.walks {
					t.Errorf("walking up, %s says %v, want %v", by, got, tt.walks)
				}
			}
			for by, got := range climb {
				if got != tt.climbs {
					t.Errorf("walking and jumping up, %s says %v, want %v", by, got, tt.climbs)
				}
			}
			if back := n.Reach([]nav.Point{up}, params.Radius, rise); !back.Near(from, 0.1) {
				t.Error("any drop is allowed, but the reach check can't get back down")
			}
		})
	}
}

// shelf is a 20 × 12 m map with the cave's kind of shelf: four 0.3 m terraces sharing their south edge, at z = 4, and stepping up from the west. Where the steps meet the cliff, its face slopes gently along x and steeply along z.
const shelf = `
size: [20, 12]
look: {ground: stone, sky: cave}
terrain:
  cell: 0.5
  mesas:
    - {area: [4, 4, 16, 8], height: 0.3, edge: 0.5}
    - {area: [5, 4, 15, 8], height: 0.3, edge: 0.5}
    - {area: [6, 4, 14, 8], height: 0.3, edge: 0.5}
    - {area: [7, 4, 13, 8], height: 0.3, edge: 0.5}
`

// A body on a cliff's face, where steps meet the cliff, walks along the face but can't climb straight out of it, in the sim or by A*: each path from the face to the top goes round by the steps, and the sim walks it at a mob's pace.
func TestAPathOffACliffFaceIsOneTheSimWalks(t *testing.T) {
	var f mapfile.File
	if err := yaml.Unmarshal([]byte(shelf), &f); err != nil {
		t.Fatal(err)
	}
	m, err := mapfile.Build("shelf", f, params)
	if err != nil {
		t.Fatal(err)
	}
	g, top := m.Grid(), nav.Point{X: 12.25, Z: 8.25}
	for _, from := range []nav.Point{{X: 9.25, Z: 3.75}, {X: 11.25, Z: 3.75}, {X: 15.25, Z: 3.75}} {
		if h := g.GroundAt(from); h < 0.5 || h > 0.7 {
			t.Fatalf("(%.2f, %.2f) stands %.2f m up; want it on the face, half way up the cliff", from.X, from.Z, h)
		}
		path := new(nav.Finder).Path(g, from, top, 0.625, from, 30)
		if path == nil {
			t.Fatalf("no way from the face at (%.2f, %.2f) to the top", from.X, from.Z)
		}
		s := sim.State{X: from.X, Y: g.GroundAt(from), Z: from.Z, OnGround: true}
		body := params
		body.Radius, body.Height = 0.625, 1
		for _, w := range path {
			for range 100 {
				dx, dz := w.X-s.X, w.Z-s.Z
				d := math.Hypot(dx, dz)
				if d < 1e-9 {
					break
				}
				step := min(0.1375, d) // a cave bat's 2.75 m/s for a 50 ms tick
				was := s
				s = sim.Push(s, dx/d*step, dz/d*step, m.Geometry, body)
				if s.X == was.X && s.Z == was.Z {
					t.Fatalf("from the face at (%.2f, %.2f), the sim stops the body at (%.2f, %.2f) short of the path's (%.2f, %.2f)", from.X, from.Z, s.X, s.Z, w.X, w.Z)
				}
				s.Y = m.Geometry.Terrain.Height(s.X, s.Z)
			}
		}
		if s.Y != 1.2 {
			t.Errorf("from the face at (%.2f, %.2f), the body ends %.2f m up; want on the top", from.X, from.Z, s.Y)
		}
	}
}

func TestClearance(t *testing.T) {
	box := sim.Box{X: 5, Z: 5, HalfW: 1, HalfD: 1, UX: 1, Top: 2}
	low := sim.Box{X: 15, Z: 5, HalfW: 1, HalfD: 1, UX: 1, Top: 0.25}
	tree := sim.Cylinder{X: 5, Z: 15, Radius: 1, Top: 6}
	n := nav.New(flat(20, 20, []sim.Box{box, low}, []sim.Cylinder{tree}), params.Step)
	tests := []struct {
		name string
		at   nav.Point
		r    float64
		fits bool
	}{
		{"0.75 m east of the box", nav.Point{X: 6.75, Z: 5.25}, 0.75, true},
		{"0.75 m east of the box, for a body 1.6 m across", nav.Point{X: 6.75, Z: 5.25}, 0.8, false},
		{"on the box", nav.Point{X: 5.25, Z: 5.25}, 0.1, false},
		{"on a box low enough to step onto", nav.Point{X: 15.25, Z: 5.25}, 0.5, true},
		{"beside the tree", nav.Point{X: 5.25, Z: 16.75}, 0.7, true},
		{"against the tree", nav.Point{X: 5.25, Z: 16.25}, 0.7, false},
		{"by the map's edge", nav.Point{X: 0.25, Z: 10.25}, 0.375, false},
		{"off the map", nav.Point{X: -1, Z: 10}, 0.1, false},
	}
	for _, tt := range tests {
		if got := n.Fits(tt.at, tt.r); got != tt.fits {
			t.Errorf("%s: fits %v, want %v", tt.name, got, tt.fits)
		}
	}
	if got := n.GroundAt(nav.Point{X: 15.25, Z: 5.25}); got != 0.25 {
		t.Errorf("the ground on the low box is %v, want its top", got)
	}
}

// path walks from to at r, failing the test unless every cell on it fits and none is up on a solid.
func path(t *testing.T, n *nav.Grid, from, to nav.Point, r float64) []nav.Point {
	t.Helper()
	p := new(nav.Finder).Path(n, from, to, r, from, 30)
	if p == nil {
		t.Fatal("no path")
	}
	for _, c := range p {
		if !n.Fits(c, r) || n.GroundAt(c) > 0.3 {
			t.Fatalf("the path goes through %v", c)
		}
	}
	if last := p[len(p)-1]; math.Hypot(last.X-to.X, last.Z-to.Z) > 0.5 {
		t.Fatalf("the path ends at %v, not %v", last, to)
	}
	return p
}

func TestPathGoesAround(t *testing.T) {
	const r = 0.5
	from, to := nav.Point{X: 2.25, Z: 10.25}, nav.Point{X: 18.25, Z: 10.25}
	t.Run("a wall of a turned box", func(t *testing.T) {
		wall := sim.Box{X: 10, Z: 10, HalfW: 0.25, HalfD: 5, UX: math.Sqrt(0.75), UZ: -0.5, Top: 3}
		path(t, nav.New(flat(20, 20, []sim.Box{wall}, nil), params.Step), from, to, r)
	})
	t.Run("a cylinder", func(t *testing.T) {
		path(t, nav.New(flat(20, 20, nil, []sim.Cylinder{{X: 10, Z: 10, Radius: 3, Top: 6}}), params.Step), from, to, r)
	})
	t.Run("a mesa's cliff edge", func(t *testing.T) {
		const nx, nz = 41, 41
		h := make([]float64, nx*nz)
		for iz := range nz {
			for ix := range nx {
				if ix >= 16 && ix <= 24 && iz >= 12 && iz <= 28 { // x 8 to 12, z 6 to 14, 1.2 m up
					h[iz*nx+ix] = 1.2
				}
			}
		}
		g := sim.Geometry{SizeX: 20, SizeZ: 20, Terrain: sim.NewTerrain(0.5, nx, nz, h, params.MaxSlope), Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}}
		p := path(t, nav.New(g, params.Step), from, to, r)
		for _, c := range p {
			if c.X > 7.5 && c.X < 12.5 && c.Z > 5.5 && c.Z < 14.5 {
				t.Fatalf("the path climbs the mesa at %v", c)
			}
		}
	})
}

// A jump over a cell needs room among the solids too tall to clear: a body can't jump a gap narrower than itself between tall walls.
func TestReachJumpsOnlyThroughRoom(t *testing.T) {
	walls := func(top float64) []sim.Box { // thin walls along z across a 3 m map, 0.5 m apart, over the cell at x = 6.25
		return []sim.Box{{X: 6.25, Z: 0.75, HalfW: 0.1, HalfD: 0.75, UX: 1, Top: top}, {X: 6.25, Z: 2.5, HalfW: 0.1, HalfD: 0.5, UX: 1, Top: top}}
	}
	from, past := nav.Point{X: 2.25, Z: 1.75}, nav.Point{X: 10.25, Z: 1.75}
	if nav.New(flat(12, 3, walls(6), nil), params.Step).Reach([]nav.Point{from}, params.Radius, rise).Near(past, 0.1) {
		t.Error("a body 0.75 m across jumps through a 0.5 m gap between walls 6 m tall")
	}
	if !nav.New(flat(12, 3, walls(1), nil), params.Step).Reach([]nav.Point{from}, params.Radius, rise).Near(past, 0.1) {
		t.Error("a body can't jump past walls 1 m tall")
	}
}

// A diagonal step needs both of its side cells, as the sim moves x then z: a wall in either one makes the path go round its corner.
func TestPathDoesntCutCorners(t *testing.T) {
	post := sim.Box{X: 10.25, Z: 10.75, HalfW: 0.25, HalfD: 0.25, UX: 1, Top: 3} // the cell north of the start
	n := nav.New(flat(20, 20, []sim.Box{post}, nil), params.Step)
	from, to := nav.Point{X: 10.25, Z: 10.25}, nav.Point{X: 10.75, Z: 10.75}
	p := new(nav.Finder).Path(n, from, to, 0.2, from, 5)
	if len(p) != 2 || p[0] != (nav.Point{X: 10.75, Z: 10.25}) {
		t.Errorf("path %v, want it east then north, round the post's corner", p)
	}
}

// A body already in to's cell has arrived: its path is that cell alone, as nil means there's no way.
func TestPathFromToItsOwnCell(t *testing.T) {
	n := nav.New(flat(10, 10, nil, nil), params.Step)
	from := nav.Point{X: 5.1, Z: 5.1}
	if p := new(nav.Finder).Path(n, from, nav.Point{X: 5.4, Z: 5.4}, params.Radius, from, 5); len(p) != 1 || p[0] != (nav.Point{X: 5.25, Z: 5.25}) {
		t.Errorf("path %v, want the cell's centre alone", p)
	}
}

func TestPathKeepsToTheLeash(t *testing.T) {
	n := nav.New(flat(40, 40, []sim.Box{{X: 20, Z: 20, HalfW: 0.5, HalfD: 6, UX: 1, Top: 3}}, nil), params.Step)
	home := nav.Point{X: 20.25, Z: 20.25}
	if new(nav.Finder).Path(n, nav.Point{X: 15.25, Z: 20.25}, nav.Point{X: 15.25, Z: 35.25}, 0.5, home, 10) != nil {
		t.Error("a path to a point past the leash")
	}
	// Around the wall's end is 7 m from home; with a 6 m leash the way round is out of bounds.
	if new(nav.Finder).Path(n, nav.Point{X: 17.25, Z: 20.25}, nav.Point{X: 23.25, Z: 20.25}, 0.5, home, 6) != nil {
		t.Error("a path that leaves the leash to go round the wall")
	}
	if new(nav.Finder).Path(n, nav.Point{X: 17.25, Z: 20.25}, nav.Point{X: 23.25, Z: 20.25}, 0.5, home, 9) == nil {
		t.Error("no path round the wall inside a 9 m leash")
	}
}

// A search keeps state only for the leash's cells, so a failing one on a big map costs what it would on a small one.
func TestPathSearchIsBounded(t *testing.T) {
	ring := []sim.Box{{X: 100, Z: 95, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3}, {X: 100, Z: 105, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3},
		{X: 97, Z: 100, HalfW: 0.25, HalfD: 5.25, UX: 1, Top: 3}, {X: 103, Z: 100, HalfW: 0.25, HalfD: 5.25, UX: 1, Top: 3}}
	n := nav.New(flat(200, 200, ring, nil), params.Step)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if new(nav.Finder).Path(n, nav.Point{X: 90.25, Z: 100.25}, nav.Point{X: 100.25, Z: 100.25}, 0.5, nav.Point{X: 95.25, Z: 100.25}, 16) != nil {
		t.Fatal("a path into the walled ring")
	}
	runtime.ReadMemStats(&after)
	// The whole grid is 160,000 cells, 1.3 MB at one float64 each; the leash's square is about 4,200, a few hundred kB of search state.
	if used := after.TotalAlloc - before.TotalAlloc; used > 1<<20 {
		t.Errorf("a failed search allocated %d bytes", used)
	}
}

// The sim's jump is stepped, so it peaks a little lower the faster it's played; the reach check takes the fastest.
func TestJumpRise(t *testing.T) {
	slow, fast := nav.JumpRise(params), nav.JumpRise(params.Faster(1.3))
	if !(fast < slow) || math.Abs(slow-1.902) > 0.001 || math.Abs(fast-1.870) > 0.001 {
		t.Errorf("rise %.4f at 1×, %.4f at 1.3×", slow, fast)
	}
}

// A body pressed against a wall stands in a cell too tight for it at its centre: its way starts at the nearest cell it fits.
func TestPathFromATooTightCellStartsWhereTheBodyFits(t *testing.T) {
	const r = 0.875
	n := nav.New(flat(40, 40, []sim.Box{{X: 20, Z: 22.5, HalfW: 4, HalfD: 0.5, UX: 1, Top: 3}}, nil), params.Step)
	from := nav.Point{X: 20.1, Z: 22 - r} // touching the wall's south face
	if n.Fits(from, r) {
		t.Fatal("the start fits; want its cell too tight at the centre")
	}
	p := path(t, n, from, nav.Point{X: 20.25, Z: 15.25}, r)
	if d := nav.Distance(from, p[0]); d > 0.75 {
		t.Fatalf("the path starts at %v, %v m away; want the nearest cell the body fits", p[0], d)
	}
}

// A search walks round the cells it's told to avoid, but never refuses its start for one.
func TestPathAvoidsCells(t *testing.T) {
	n := nav.New(flat(40, 40, nil, nil), params.Step)
	from, to, blocked := nav.Point{X: 15.25, Z: 20.25}, nav.Point{X: 25.25, Z: 20.25}, nav.Point{X: 20.25, Z: 20.25}
	f := new(nav.Finder)
	p := f.Path(n, from, to, 0.5, from, 30, blocked, from)
	if p == nil || p[len(p)-1] != to {
		t.Fatalf("path %v; want one to %v", p, to)
	}
	for _, c := range p {
		if c == blocked {
			t.Fatalf("the path %v goes through the avoided cell", p)
		}
	}
	if f.Path(n, from, to, 0.5, from, 30, to) != nil {
		t.Error("a path ending in an avoided cell")
	}
}

// A leash that isn't a finite number of metres has no cells inside it.
func TestPathRefusesALeashThatIsntFinite(t *testing.T) {
	n := nav.New(flat(40, 40, nil, nil), params.Step)
	for _, leash := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if p := new(nav.Finder).Path(n, nav.Point{X: 5.25, Z: 5.25}, nav.Point{X: 10.25, Z: 5.25}, 0.5, nav.Point{X: 5, Z: 5}, leash); p != nil {
			t.Errorf("leash %v: path %v", leash, p)
		}
	}
}

// One finder's searches answer as fresh ones do, whatever ran before them, and once warm allocate only their answer.
func TestAFinderReusesItsBuffers(t *testing.T) {
	n := nav.New(flat(40, 40, []sim.Box{{X: 20, Z: 20, HalfW: 0.5, HalfD: 6, UX: 1, Top: 3}}, nil), params.Step)
	type search struct {
		from, to, home nav.Point
		leash          float64
	}
	searches := []search{
		{nav.Point{X: 17.25, Z: 20.25}, nav.Point{X: 23.25, Z: 20.25}, nav.Point{X: 20.25, Z: 20.25}, 9},
		{nav.Point{X: 5.25, Z: 5.25}, nav.Point{X: 35.25, Z: 35.25}, nav.Point{X: 20, Z: 20}, 30},
		{nav.Point{X: 17.25, Z: 20.25}, nav.Point{X: 23.25, Z: 20.25}, nav.Point{X: 20.25, Z: 20.25}, 6}, // none: the leash cuts the way round
	}
	f := new(nav.Finder)
	for round := range 3 {
		for i, s := range searches {
			got, want := f.Path(n, s.from, s.to, 0.5, s.home, s.leash), new(nav.Finder).Path(n, s.from, s.to, 0.5, s.home, s.leash)
			if !slices.Equal(got, want) {
				t.Fatalf("round %d, search %d: %v; a fresh finder found %v", round, i, got, want)
			}
		}
	}
	s := searches[0]
	if allocs := testing.AllocsPerRun(20, func() { f.Path(n, s.from, s.to, 0.5, s.home, s.leash) }); allocs > 1 {
		t.Errorf("a warm search allocated %v times; want only its path", allocs)
	}
}
