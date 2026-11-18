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

// A move reads the buckets it sweeps into, not only those the body stands in: walking each way toward a cylinder 2 cm across the bucket line at 16 m, from 2.5 cm short of it, a body stops at the cylinder as a full scan stops it.
func TestAMoveReadsTheBucketsItSweepsInto(t *testing.T) {
	const line, r, gap, short = 16.0, 0.4, 0.02, 0.025
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
		t.Run(tt.name, func(t *testing.T) {
			g := ground(32, 32, 32, func(_, _ float64) float64 { return 0 })
			g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, params.MaxSlope)
			g.Cylinders = []sim.Cylinder{{X: tt.cx, Z: tt.cz, Radius: r, Top: 2}}
			ix := sim.Indexed(g)
			s, t2 := standing(tt.x, 0, tt.z), standing(tt.x, 0, tt.z)
			for range 6 {
				s, t2 = sim.Step(s, tt.in, g, params), sim.Step(t2, tt.in, ix, params)
			}
			if s != t2 || math.Hypot(s.X-tt.cx, s.Z-tt.cz) < r+params.Radius-1e-9 || math.Hypot(s.X-tt.x, s.Z-tt.z) > short+gap+1e-9 {
				t.Fatalf("walked to %+v scanned, %+v indexed; want both stopped at the cylinder", s, t2)
			}
		})
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
