package sim_test

import (
	"fmt"
	"math"
	"testing"

	"pgregory.net/rapid"

	"finga/internal/shared/sim"
	"finga/internal/shared/sim/simtest"
)

// large-regions.md section 5: an Indexed geometry answers every query as a full scan does, to the bit, and bodies walking, jumping, blinking and pushed among its solids move the same.
func TestAnIndexedGeometryAnswersAsAFullScan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := params
		g := simtest.Geometry(t, p.MaxSlope)
		ix := sim.Indexed(g)
		for range 20 {
			x, z := simtest.Near(t, g)
			y := rapid.Float64Range(-2, 5).Draw(t, "y")
			if a, b := sim.Ground(x, z, y, g), sim.Ground(x, z, y, ix); a != b {
				t.Fatalf("ground at (%v, %v) below %v: %v scanned, %v indexed", x, z, y, a, b)
			}
			if a, b := g.SolidOver(x, z, y), ix.SolidOver(x, z, y); a != b {
				t.Fatalf("a solid over %v at (%v, %v): %v scanned, %v indexed", y, x, z, a, b)
			}
		}
		x, z := simtest.Near(t, g)
		s := sim.State{X: x, Y: sim.Ground(x, z, math.Inf(1), g), Z: z, OnGround: true, HZ: 1}
		var in sim.Input
		for k := range 200 {
			var a, b sim.State
			switch rapid.IntRange(0, 15).Draw(t, "what") {
			case 0:
				turn := rapid.Float64Range(0, 2*math.Pi).Draw(t, "way")
				w := sim.Way{X: math.Cos(turn), Z: math.Sin(turn), Length: rapid.Float64Range(0, 12).Draw(t, "length")}
				a, b = sim.Blink(s, w, g, p), sim.Blink(s, w, ix, p)
			case 1:
				dx, dz := rapid.Float64Range(-1, 1).Draw(t, "dx"), rapid.Float64Range(-1, 1).Draw(t, "dz")
				a, b = sim.Push(s, dx, dz, g, p), sim.Push(s, dx, dz, ix, p)
			case 2: // new keys, held until the next: a body walks straight a while
				in = sim.Input{Left: rapid.Bool().Draw(t, "left"), Right: rapid.Bool().Draw(t, "right"), Up: rapid.Bool().Draw(t, "up"), Down: rapid.Bool().Draw(t, "down"), Jump: rapid.Bool().Draw(t, "jump")}
				fallthrough
			default:
				a, b = sim.Step(s, in, g, p), sim.Step(s, in, ix, p)
			}
			if a != b {
				t.Fatalf("move %d from %+v: %+v scanned, %+v indexed", k, s, a, b)
			}
			s = a
		}
	})
}

// large-regions.md section 5: a geometry Blocking with bucketed blockers moves bodies as a full scan of them does, to the bit: walking, jumping, through mobs or not, blinking and pushed, with one index rebuilt for each crowd.
func TestBlockingMovesAsAFullScan(t *testing.T) {
	var blocking sim.Blockers // reused from draw to draw, as the world reuses its own
	rapid.Check(t, func(t *rapid.T) {
		p := params
		g := sim.Indexed(simtest.Geometry(t, p.MaxSlope))
		blocking.Set(g.Blockers, g.SizeX, g.SizeZ)
		ix := g.Blocking(&blocking)
		x, z := simtest.Near(t, g)
		s := sim.State{X: x, Y: sim.Ground(x, z, math.Inf(1), g), Z: z, OnGround: true, HZ: 1}
		var in sim.Input
		for k := range 100 {
			var a, b sim.State
			switch rapid.IntRange(0, 15).Draw(t, "what") {
			case 0:
				turn := rapid.Float64Range(0, 2*math.Pi).Draw(t, "way")
				w := sim.Way{X: math.Cos(turn), Z: math.Sin(turn), Length: rapid.Float64Range(0, 12).Draw(t, "length")}
				a, b = sim.Blink(s, w, g, p), sim.Blink(s, w, ix, p)
			case 1:
				dx, dz := rapid.Float64Range(-1, 1).Draw(t, "dx"), rapid.Float64Range(-1, 1).Draw(t, "dz")
				a, b = sim.Push(s, dx, dz, g, p), sim.Push(s, dx, dz, ix, p)
			case 2: // a skill's move, through mobs or not
				turn := rapid.Float64Range(0, 2*math.Pi).Draw(t, "move way")
				s.Move = sim.Move{VX: 9 * math.Cos(turn), VZ: 9 * math.Sin(turn), Left: rapid.IntRange(1, 30).Draw(t, "move left"), Through: rapid.Bool().Draw(t, "through")}
				a, b = sim.Step(s, in, g, p), sim.Step(s, in, ix, p)
			case 3: // new keys, held until the next: a body walks straight a while
				in = sim.Input{Left: rapid.Bool().Draw(t, "left"), Right: rapid.Bool().Draw(t, "right"), Up: rapid.Bool().Draw(t, "up"), Down: rapid.Bool().Draw(t, "down"), Jump: rapid.Bool().Draw(t, "jump")}
				fallthrough
			default:
				a, b = sim.Step(s, in, g, p), sim.Step(s, in, ix, p)
			}
			if a != b {
				t.Fatalf("move %d from %+v among %d blockers: %+v scanned, %+v bucketed", k, s, len(g.Blockers), a, b)
			}
			s = a
		}
	})
}

// An index is read only for its own list: a geometry whose Blockers were set past Blocking steps against them as they are, whether another list, the one indexed grown in place, or that one refilled without a Set.
func TestABlockersIndexIsReadOnlyForItsOwnList(t *testing.T) {
	g := ground(96, 96, 32, func(_, _ float64) float64 { return 0 })
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
	far := sim.Blocker{X: 80, Z: 80, Radius: 0.5, Bottom: -1, Top: 2} // in another bucket than the walk
	near := sim.Blocker{X: 6, Z: 5, Radius: 0.5, Bottom: -1, Top: 2}
	for _, tt := range []struct {
		name string
		set  func(indexed []sim.Blocker) []sim.Blocker
	}{
		{"another list of as many", func([]sim.Blocker) []sim.Blocker { return []sim.Blocker{near} }},
		{"the indexed list grown in place", func(indexed []sim.Blocker) []sim.Blocker { return append(indexed, near) }},
		{"the indexed list refilled without a Set", func(indexed []sim.Blocker) []sim.Blocker { indexed[0] = near; return indexed }},
	} {
		indexed := append(make([]sim.Blocker, 0, 4), far)
		var blocking sim.Blockers
		blocking.Set(indexed, g.SizeX, g.SizeZ)
		ix := g.Blocking(&blocking)
		ix.Blockers = tt.set(indexed)
		plain := g
		plain.Blockers = ix.Blockers
		s := sim.State{X: 4, Z: 5, OnGround: true, HX: 1}
		for range 30 {
			a, b := sim.Step(s, sim.Input{Right: true}, plain, params), sim.Step(s, sim.Input{Right: true}, ix, params)
			if a != b {
				t.Fatalf("%s: walking east from %+v: %+v as it is, %+v through the old index", tt.name, s, a, b)
			}
			s = a
		}
		if s.X > 6 {
			t.Fatalf("%s: walked to x %v, through the blocker at 6", tt.name, s.X)
		}
	}
}

// An index never Set, as a frame not yet kept lends, blocks nothing.
func TestABlockersIndexNeverSetBlocksNothing(t *testing.T) {
	g := ground(32, 32, 32, func(_, _ float64) float64 { return 0 })
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
	var never sim.Blockers
	from := standing(4, 0, 5)
	if a, b := sim.Step(from, sim.Input{Right: true}, g, params), sim.Step(from, sim.Input{Right: true}, g.Blocking(&never), params); a != b {
		t.Fatalf("a step east: %+v with no blockers, %+v with an index never Set", a, b)
	}
}

// A move sweeping more buckets than a walk merges reads every solid, as a scan: a push 150 m east across a 200 m map, past a trunk beside its way in every bucket, stops at the trunk 120 m on.
func TestAPushPastSixteenBucketsReadsEverySolid(t *testing.T) {
	g := ground(200, 8, 8, func(_, _ float64) float64 { return 0 })
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
	for x := 4.0; x < 200; x += 8 {
		g.Cylinders = append(g.Cylinders, sim.Cylinder{X: x, Z: 7, Radius: 0.5, Top: 2})
	}
	g.Cylinders = append(g.Cylinders, sim.Cylinder{X: 130, Z: 4, Radius: 0.5, Top: 2})
	from := standing(10, 0, 4)
	a, b := sim.Push(from, 150, 0, g, params), sim.Push(from, 150, 0, sim.Indexed(g), params)
	if a != b || a.X > 130 {
		t.Fatalf("pushed to x %v scanned, %v indexed; want both stopped at the trunk at 130", a.X, b.X)
	}
}

// A blink ends short of a mob's body across a bucket line, though the mob's own bounds stay in the next bucket: the end tests the body's reach, not its centre.
func TestABlinkEndsShortOfAMobAcrossABucketLine(t *testing.T) {
	g := ground(64, 64, 32, func(_, _ float64) float64 { return 0 })
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
	g.Blockers = []sim.Blocker{{X: 32.3, Z: 5, Radius: 0.2, Bottom: -1, Top: 2}} // from 32.1 m on, past the line at 32
	var blocking sim.Blockers
	blocking.Set(g.Blockers, g.SizeX, g.SizeZ)
	from := standing(29.9, 0, 5)
	w := sim.Way{X: 1, Length: 2} // its last test, at 31.9, is inside the mob
	a, b := sim.Blink(from, w, g, params), sim.Blink(from, w, g.Blocking(&blocking), params)
	if a != b || a.X > 31.7 {
		t.Fatalf("blinked to x %v scanned, %v bucketed; want both short of the mob, at 31.65", a.X, b.X)
	}
}

// A NaN in a body or a point, as a broken state might hold, gets a full scan's answer from an Indexed geometry, and no panic.
func TestANaNGetsAFullScansAnswer(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := params
		g := simtest.Geometry(t, p.MaxSlope)
		ix := sim.Indexed(g)
		x, z := simtest.Near(t, g)
		switch rapid.IntRange(0, 2).Draw(t, "nan") {
		case 0:
			x = math.NaN()
		case 1:
			z = math.NaN()
		default:
			x, z = math.NaN(), math.NaN()
		}
		same := func(what string, a, b any) {
			if fmt.Sprint(a) != fmt.Sprint(b) {
				t.Fatalf("%s at (%v, %v): %+v scanned, %+v indexed", what, x, z, a, b)
			}
		}
		same("ground", sim.Ground(x, z, 2, g), sim.Ground(x, z, 2, ix))
		same("a solid over", g.SolidOver(x, z, 0), ix.SolidOver(x, z, 0))
		s := sim.State{X: x, Z: z, OnGround: true, HZ: 1}
		in := sim.Input{Left: rapid.Bool().Draw(t, "left"), Up: rapid.Bool().Draw(t, "up"), Jump: rapid.Bool().Draw(t, "jump")}
		same("a step", sim.Step(s, in, g, p), sim.Step(s, in, ix, p))
		same("a blink", sim.Blink(s, sim.Way{X: 0.6, Z: 0.8, Length: 5}, g, p), sim.Blink(s, sim.Way{X: 0.6, Z: 0.8, Length: 5}, ix, p))
		same("a push", sim.Push(s, 0.5, -0.5, g, p), sim.Push(s, 0.5, -0.5, ix, p))
	})
}

// A move reads the buckets it sweeps into, not only those the body stands in: walking each way toward a cylinder, a box or a mob's blocker 2 cm across the line at 32 m, which both kinds of bucket share, from 2.5 cm short of it, a body stops at it as a full scan stops it.
func TestAMoveReadsTheBucketsItSweepsInto(t *testing.T) {
	const line, r, gap, short = 32.0, 0.4, 0.02, 0.025
	reach := r + gap // from the cylinder's centre to the line
	from := params.Radius + short
	for _, tt := range []struct {
		name         string
		in           sim.Input
		cx, cz, x, z float64
	}{
		{"east", east, line + reach, 12, line - from, 12},
		{"west", west, line - reach, 12, line + from, 12},
		{"north", north, 12, line + reach, 12, line - from},
		{"south", south, 12, line - reach, 12, line + from},
	} {
		for _, kind := range []string{"cylinder", "box", "blocker"} {
			t.Run(tt.name+" "+kind, func(t *testing.T) {
				g := ground(64, 64, 32, func(_, _ float64) float64 { return 0 })
				g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
				var ix sim.Geometry
				switch kind {
				case "blocker":
					g.Blockers = []sim.Blocker{{X: tt.cx, Z: tt.cz, Radius: r, Bottom: -1, Top: 2}}
					var blocking sim.Blockers
					blocking.Set(g.Blockers, g.SizeX, g.SizeZ)
					ix = g.Blocking(&blocking)
				case "box": // a square as wide as the cylinder, its near side where the cylinder's is
					g.Boxes = []sim.Box{{X: tt.cx, Z: tt.cz, HalfW: r, HalfD: r, UX: 1, Top: 2}}
					ix = sim.Indexed(g)
				default:
					g.Cylinders = []sim.Cylinder{{X: tt.cx, Z: tt.cz, Radius: r, Top: 2}}
					ix = sim.Indexed(g)
				}
				s, t2 := standing(tt.x, 0, tt.z), standing(tt.x, 0, tt.z)
				for range 6 {
					s, t2 = sim.Step(s, tt.in, g, params), sim.Step(t2, tt.in, ix, params)
				}
				if s != t2 || math.Hypot(s.X-tt.cx, s.Z-tt.cz) < r+params.Radius-1e-9 || math.Hypot(s.X-tt.x, s.Z-tt.z) > short+gap+1e-9 {
					t.Fatalf("walked to %+v scanned, %+v indexed; want both stopped at it", s, t2)
				}
			})
		}
	}
}

// A box can hold a point a rounding step outside its bounds as Indexed works them out: this one's start on the bucket line at 16 m, yet it holds a corner at 15.999999999999998, in the bucket before, which the bounds' pad lists it in.
func TestABoxHoldingAPointPastItsBoundsIsFoundThere(t *testing.T) {
	g := ground(32, 32, 32, func(_, _ float64) float64 { return 0 })
	g.Boxes = []sim.Box{{X: 19.43359508418376, Z: 12, HalfW: 3.375, HalfD: 4, UX: -0.9998913537933956, UZ: 0.01474044128276302, Top: 1}}
	x, z := 15.999999999999998, 8.050183574155742
	if !g.Boxes[0].Holds(x, z) {
		t.Fatal("the box no longer holds its corner past the line")
	}
	if a, b := sim.Ground(x, z, 2, g), sim.Ground(x, z, 2, sim.Indexed(g)); a != 1 || b != 1 {
		t.Fatalf("ground at the corner: %v scanned, %v indexed; want the box's top, 1", a, b)
	}
}

// A solid is listed in every bucket it reaches: a box at each turn and a cylinder, centred 2 m short of the bucket lines at 16 m and reaching past them, give a full scan's ground and solid-over every 5 cm around them.
func TestASolidIsFoundInEveryBucketItReaches(t *testing.T) {
	var shapes []sim.Geometry
	for _, turn := range []float64{0, 0.5, math.Pi / 4, 1.2, math.Pi / 2, 2.3, 3 * math.Pi / 4} {
		g := ground(32, 32, 32, func(_, _ float64) float64 { return 0 })
		g.Boxes = []sim.Box{{X: 14, Z: 14, HalfW: 2.5, HalfD: 0.8, UX: math.Cos(turn), UZ: math.Sin(turn), Top: 1}}
		shapes = append(shapes, g)
	}
	g := ground(32, 32, 32, func(_, _ float64) float64 { return 0 })
	g.Cylinders = []sim.Cylinder{{X: 14, Z: 14, Radius: 2.5, Top: 1}}
	shapes = append(shapes, g)
	for _, g := range shapes {
		ix := sim.Indexed(g)
		for x := 10.0; x <= 18; x += 0.05 {
			for z := 10.0; z <= 18; z += 0.05 {
				if a, b := sim.Ground(x, z, 2, g), sim.Ground(x, z, 2, ix); a != b {
					t.Fatalf("%+v %+v: ground at (%v, %v) %v scanned, %v indexed", g.Boxes, g.Cylinders, x, z, a, b)
				}
				if a, b := g.SolidOver(x, z, 0.5), ix.SolidOver(x, z, 0.5); a != b {
					t.Fatalf("%+v %+v: a solid over (%v, %v) %v scanned, %v indexed", g.Boxes, g.Cylinders, x, z, a, b)
				}
			}
		}
	}
}
