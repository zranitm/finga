package nav_test

import (
	"testing"

	"finga/internal/gamedata/gamedatatest"
	"finga/internal/sim"
)

// A player escapes a crowd by jumping, so every mob but a field boss must be one a player gets past: walking east into it on flat ground
// and jumping at the best moment, pressed against it included, at the slowest move speed and at the fastest, where the jump peaks lowest.
// A blocker holds a body while its feet are below the mob's top, and from inside its circle only from moving closer to its centre.
func TestEveryShippedMobButABossCanBeJumpedOver(t *testing.T) {
	data := gamedatatest.Shipped(t)
	slowest := data.Movement()
	fastest := slowest.Faster(sim.SpeedFactor(data.StatRules().Curves.MoveSpeed.Limit))
	for _, m := range data.Mobs() {
		if m.Boss {
			continue
		}
		for _, p := range []sim.Params{slowest, fastest} {
			if !jumpsOver(p, m.Radius, m.Height) {
				t.Errorf("%s, %.4g m tall and %.4g m in radius: no jump at %.4g m/s gets a player past its centre; at that radius one clears %.3f m",
					m.ID, m.Height, m.Radius, p.WalkSpeed, tallestJumped(p, m.Radius))
			}
		}
	}
}

// jumpsOver says whether a player walking east into a mob of radius r and height h gets past its centre with a jump made at some sub-step.
func jumpsOver(p sim.Params, r, h float64) bool {
	const centre, z = 10.0, 10.0
	g := sim.Geometry{SizeX: 20, SizeZ: 20, Terrain: sim.NewTerrain(20, 2, 2, []float64{0, 0, 0, 0}, p.MaxSlope),
		Blockers: []sim.Blocker{{X: centre, Z: z, Radius: r, Bottom: 0, Top: h}}}
	touch := centre - r - p.Radius
	start := touch - 2*p.WalkSpeed*p.JumpVelocity/p.Gravity // more run-up than a jump's flight covers
	walk := sim.Input{Right: true}
	for jumpAt := 0; ; jumpAt++ {
		s := sim.State{X: start, Z: z, OnGround: true}
		for range jumpAt {
			s = sim.Step(s, walk, g, p)
		}
		pressed := s.X >= touch // walking alone has reached the mob, so this jump is the last one to try
		s = sim.Step(s, sim.Input{Right: true, Jump: true}, g, p)
		for !s.OnGround {
			if s = sim.Step(s, walk, g, p); s.X > centre {
				return true
			}
		}
		if pressed {
			return false
		}
	}
}

// tallestJumped is the tallest mob of radius r a player gets past, to a millimetre.
func tallestJumped(p sim.Params, r float64) float64 {
	lo, hi := 0.0, 3.0
	for hi-lo > 0.001 {
		if mid := (lo + hi) / 2; jumpsOver(p, r, mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}
