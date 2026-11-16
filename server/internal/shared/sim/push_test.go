package sim_test

import (
	"math"
	"testing"

	"finga/internal/shared/sim"
)

// A push moves a body along x, then z, each stopped where a step of the sim would stop: a solid's face, a blocker, the map's edge.
func TestPushStopsWhereAStepWould(t *testing.T) {
	p := sim.Params{Dt: 1.0 / 60, WalkSpeed: 5, JumpVelocity: 12.34, Gravity: 38, MaxFallSpeed: 22.5, Radius: 0.5, Height: 1, Step: 0.3, MaxSlope: 0.7, Snap: 0.5}
	g := sim.Geometry{SizeX: 20, SizeZ: 20, Terrain: sim.NewTerrain(20, 2, 2, []float64{0, 0, 0, 0}, p.MaxSlope),
		Boxes: []sim.Box{{X: 12, Z: 10, HalfW: 1, HalfD: 1, UX: 1, Top: 2}}, Cylinders: []sim.Cylinder{}}
	at := sim.State{X: 8, Z: 10, OnGround: true}
	blocked := g
	blocked.Blockers = []sim.Blocker{{X: 8, Z: 13, Radius: 0.5, Bottom: 0, Top: 1}}
	overhead := g
	overhead.Blockers = []sim.Blocker{{X: 8, Z: 13, Radius: 0.5, Bottom: 1.5, Top: 3}}
	for _, tt := range []struct {
		name   string
		g      sim.Geometry
		dx, dz float64
		x, z   float64
	}{
		{"into a box's face", g, 4, 0, 10.5, 10},
		{"along x into the box, then on along z", g, 4, 2, 10.5, 12},
		{"into a blocker", blocked, 0, 4, 8, 12},
		{"under a blocker that's over its head", overhead, 0, 4, 8, 14},
		{"into the map's edge", g, -10, 0, 0.5, 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := sim.Push(at, tt.dx, tt.dz, tt.g, p)
			if math.Abs(s.X-tt.x) > 1e-9 || math.Abs(s.Z-tt.z) > 1e-9 || s.Y != 0 {
				t.Fatalf("pushed to (%v, %v, %v); want (%v, 0, %v)", s.X, s.Y, s.Z, tt.x, tt.z)
			}
		})
	}
}
