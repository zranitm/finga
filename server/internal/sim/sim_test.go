package sim_test

import (
	"math"
	"testing"

	"finga/internal/sim"
)

// A flat floor 1000 px wide whose top is at y = 500, with a wall on the
// right (x 700..732) and a ceiling block above x 300..400 at height 420..440.
var flat = sim.Geometry{
	Solids: []sim.Rect{
		{X: 0, Y: 500, W: 1000, H: 32},  // floor
		{X: 700, Y: 300, W: 32, H: 200}, // wall
		{X: 300, Y: 420, W: 100, H: 20}, // low ceiling: 80 px above the floor, 24 px above the character's head
	},
}

func standing(x float64) sim.State {
	return sim.State{X: x, Y: 500, Facing: 1, OnGround: true, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
}

func run(s sim.State, in sim.Input, g sim.Geometry, n int) sim.State {
	p := sim.DefaultParams()
	for range n {
		s = sim.Step(s, in, g, p)
	}
	return s
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestWalkOnFlatGround(t *testing.T) {
	tests := []struct {
		name       string
		in         sim.Input
		steps      int
		wantX      float64
		wantFacing int
	}{
		{"right for one second covers walk speed", sim.Input{Right: true}, 60, 400, 1},
		{"left for one second covers walk speed", sim.Input{Left: true}, 60, 0, -1},
		{"both held: no movement, facing unchanged", sim.Input{Left: true, Right: true}, 60, 200, 1},
		{"nothing held: stands still", sim.Input{}, 60, 200, 1},
		{"one sub-step moves walk/60", sim.Input{Right: true}, 1, 200 + 200.0/60, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := run(standing(200), tt.in, flat, tt.steps)
			if !near(s.X, tt.wantX, 1e-9) {
				t.Fatalf("x = %v, want %v", s.X, tt.wantX)
			}
			if s.Y != 500 || !s.OnGround || s.VY != 0 {
				t.Fatalf("left the floor: %+v", s)
			}
			if s.Facing != tt.wantFacing {
				t.Fatalf("facing = %d, want %d", s.Facing, tt.wantFacing)
			}
		})
	}
}

func TestJump(t *testing.T) {
	p := sim.DefaultParams()
	s := standing(200)
	peak := s.Y
	landed := -1
	for i := 1; i <= 120; i++ {
		s = sim.Step(s, sim.Input{Jump: i == 1}, flat, p) // tap jump on the first sub-step only
		peak = math.Min(peak, s.Y)
		if i > 1 && s.OnGround {
			landed = i
			break
		}
	}
	// Analytical peak is v²/2g = 101.2 px; with velocity integrated before
	// position it comes out a little lower.
	rise := 500 - peak
	if rise < 95 || rise > 101.2 {
		t.Fatalf("jump rose %v px, want about 100", rise)
	}
	// Flight time is about 2v/g = 0.65 s, 39 sub-steps, plus the landing step.
	if landed < 38 || landed > 42 {
		t.Fatalf("landed after %d sub-steps, want about 40", landed)
	}
	if s.Y != 500 || s.VY != 0 || s.X != 200 {
		t.Fatalf("landing state %+v, want back on the floor at the same x", s)
	}
}

func TestJumpOnlyFromTheGround(t *testing.T) {
	p := sim.DefaultParams()
	s := sim.Step(standing(200), sim.Input{Jump: true}, flat, p)
	vyAfterJump := s.VY
	s = sim.Step(s, sim.Input{Jump: true}, flat, p) // still held in the air
	if s.VY <= vyAfterJump {
		t.Fatalf("a second jump started in the air: vy %v after %v", s.VY, vyAfterJump)
	}
}

func TestWalkingOffALedge(t *testing.T) {
	ledge := sim.Geometry{Solids: []sim.Rect{{X: 0, Y: 500, W: 200, H: 32}}}
	s := run(standing(150), sim.Input{Right: true}, ledge, 30) // 100 px right: past the edge at 200+12
	if s.OnGround {
		t.Fatalf("still on the ground past the ledge: %+v", s)
	}
	if s.Y <= 500 || s.VY <= 0 {
		t.Fatalf("not falling: %+v", s)
	}
	if !near(s.X, 250, 1e-9) {
		t.Fatalf("x = %v, want 250: walking continues in the air", s.X)
	}
}

func TestFallSpeedIsClamped(t *testing.T) {
	p := sim.DefaultParams()
	s := sim.State{X: 100, Y: 0, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
	s = run(s, sim.Input{}, sim.Geometry{}, 120) // two seconds of free fall, nothing to land on
	if s.VY != p.MaxFallSpeed {
		t.Fatalf("vy = %v, want the clamp %v", s.VY, p.MaxFallSpeed)
	}
	if s.OnGround {
		t.Fatal("on the ground with nothing below")
	}
}

func TestWallsAndCeilings(t *testing.T) {
	t.Run("walking into a wall stops at its face", func(t *testing.T) {
		s := run(standing(600), sim.Input{Right: true}, flat, 60) // would reach 800; the wall face is at 700
		if s.X != 700-12 || s.VX != 0 {
			t.Fatalf("x = %v vx = %v, want %v and 0", s.X, s.VX, 700-12)
		}
		if !s.OnGround {
			t.Fatal("left the ground")
		}
	})
	t.Run("jumping into a ceiling stops at its underside", func(t *testing.T) {
		p := sim.DefaultParams()
		s := standing(350) // under the low ceiling: head at 444, ceiling underside at 440
		minY := s.Y
		hitCeiling := false
		for i := 1; i <= 60; i++ {
			s = sim.Step(s, sim.Input{Jump: i == 1}, flat, p)
			minY = math.Min(minY, s.Y)
			if s.Y == 440+p.Height && s.VY == 0 {
				hitCeiling = true
			}
		}
		if !hitCeiling {
			t.Fatalf("never stopped at the ceiling; highest feet y %v, want %v", minY, 440+p.Height)
		}
		if minY < 440+p.Height {
			t.Fatalf("passed into the ceiling: feet reached %v", minY)
		}
		if !s.OnGround || s.Y != 500 {
			t.Fatalf("did not come back down: %+v", s)
		}
	})
}

func TestOneWayPlatforms(t *testing.T) {
	// Floor at 500 and a one-way platform 80 px above it, x 100..300.
	g := sim.Geometry{
		Solids:    []sim.Rect{{X: 0, Y: 500, W: 1000, H: 32}},
		Platforms: []sim.Rect{{X: 100, Y: 420, W: 200, H: 8}},
	}
	p := sim.DefaultParams()

	t.Run("landing on it from above", func(t *testing.T) {
		s := sim.State{X: 200, Y: 300, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
		s = run(s, sim.Input{}, g, 60)
		if !s.OnGround || s.Y != 420 || s.Ground != 0 {
			t.Fatalf("state %+v, want standing on platform 0 at y 420", s)
		}
	})

	t.Run("jumping up through it from below", func(t *testing.T) {
		s := standing(200) // on the floor, under the platform
		passedThrough := false
		for i := 1; i <= 120; i++ {
			s = sim.Step(s, sim.Input{Jump: i == 1}, g, p)
			if s.Y < 420 {
				passedThrough = true
			}
			if passedThrough && s.OnGround {
				break
			}
		}
		if !passedThrough {
			t.Fatal("the platform blocked an upward jump")
		}
		if !s.OnGround || s.Y != 420 || s.Ground != 0 {
			t.Fatalf("state %+v, want landed on the platform after passing through", s)
		}
	})

	t.Run("dropping through with Down and Jump", func(t *testing.T) {
		s := sim.State{X: 200, Y: 420, Facing: 1, OnGround: true, Ground: 0, DropPlatform: sim.NoPlatform}
		s = sim.Step(s, sim.Input{Down: true, Jump: true}, g, p)
		if s.OnGround || s.DropPlatform != 0 || s.DropTimer <= 0 {
			t.Fatalf("after Down+Jump: %+v, want falling with platform 0 ignored", s)
		}
		if s.VY < 0 {
			t.Fatalf("Down+Jump jumped instead of dropping: vy %v", s.VY)
		}
		s = run(s, sim.Input{}, g, 60)
		if !s.OnGround || s.Y != 500 || s.Ground != sim.NoPlatform {
			t.Fatalf("state %+v, want standing on the floor below", s)
		}
		if s.DropPlatform != sim.NoPlatform || s.DropTimer != 0 {
			t.Fatalf("drop-through did not expire: %+v", s)
		}
	})

	t.Run("Down alone on a solid floor does nothing", func(t *testing.T) {
		s := sim.Step(standing(200), sim.Input{Down: true, Jump: true}, g, p)
		if s.VY >= 0 || s.DropPlatform != sim.NoPlatform {
			t.Fatalf("Down+Jump on a solid floor should jump: %+v", s)
		}
	})

	t.Run("standing on the very edge", func(t *testing.T) {
		tests := []struct {
			name     string
			x        float64
			wantHold bool
		}{
			{"one pixel of overlap on the left end holds", 100 - p.Width/2 + 1, true},
			{"box flush with the left end falls", 100 - p.Width/2, false},
			{"one pixel of overlap on the right end holds", 300 + p.Width/2 - 1, true},
			{"box flush with the right end falls", 300 + p.Width/2, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s := sim.State{X: tt.x, Y: 420, Facing: 1, OnGround: true, Ground: 0, DropPlatform: sim.NoPlatform}
				s = sim.Step(s, sim.Input{}, g, p)
				if s.OnGround != tt.wantHold {
					t.Fatalf("on ground = %v, want %v (state %+v)", s.OnGround, tt.wantHold, s)
				}
			})
		}
	})
}

func TestRopes(t *testing.T) {
	// Floor at 500; a platform at 300 (x 0..400) with a rope from its top
	// down to the floor at x 196..204.
	g := sim.Geometry{
		Solids:    []sim.Rect{{X: 0, Y: 500, W: 1000, H: 32}},
		Platforms: []sim.Rect{{X: 0, Y: 300, W: 400, H: 8}},
		Ropes:     []sim.Rect{{X: 196, Y: 300, W: 8, H: 200}},
	}
	p := sim.DefaultParams()

	t.Run("Up on a rope starts climbing, snapped to its centre, without gravity", func(t *testing.T) {
		s := run(standing(190), sim.Input{Up: true}, g, 30) // half a second
		if !s.Climbing {
			t.Fatalf("not climbing: %+v", s)
		}
		if s.X != 200 {
			t.Fatalf("x = %v, want the rope centre 200", s.X)
		}
		if !near(s.Y, 500-p.ClimbSpeed*0.5, 1e-9) {
			t.Fatalf("y = %v, want %v (climbed at climb speed)", s.Y, 500-p.ClimbSpeed*0.5)
		}
		if s.OnGround || s.VY != 0 {
			t.Fatalf("state %+v", s)
		}
	})

	t.Run("Down climbs down and Up then Down is a no-op pair", func(t *testing.T) {
		s := run(standing(200), sim.Input{Up: true}, g, 30)
		s = run(s, sim.Input{Down: true}, g, 10)
		if !s.Climbing || !near(s.Y, 500-p.ClimbSpeed*(0.5-10.0/60), 1e-9) {
			t.Fatalf("state %+v", s)
		}
	})

	t.Run("climbing past the top lands on the platform there", func(t *testing.T) {
		s := run(standing(200), sim.Input{Up: true}, g, 120) // two seconds: more than enough for 200 px at 140 px/s
		if s.Climbing {
			t.Fatal("still climbing past the top")
		}
		if !s.OnGround || s.Y != 300 || s.Ground != 0 {
			t.Fatalf("state %+v, want standing on the platform at 300", s)
		}
	})

	t.Run("climbing below the bottom falls off", func(t *testing.T) {
		s := sim.State{X: 200, Y: 400, Facing: 1, Climbing: true, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
		s = run(s, sim.Input{Down: true}, g, 60) // 140 px down: past the floor-level bottom
		if s.Climbing {
			t.Fatal("still climbing below the rope")
		}
		if !s.OnGround || s.Y != 500 {
			t.Fatalf("state %+v, want standing on the floor", s)
		}
	})

	t.Run("Jump leaves the rope with a jump", func(t *testing.T) {
		s := sim.State{X: 200, Y: 400, Facing: 1, Climbing: true, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
		s = sim.Step(s, sim.Input{Jump: true, Right: true}, g, p)
		if s.Climbing || s.VY >= 0 || s.VX <= 0 {
			t.Fatalf("state %+v, want jumping off to the right", s)
		}
		s = run(s, sim.Input{Right: true}, g, 120)
		if !s.OnGround || s.Y != 500 {
			t.Fatalf("state %+v, want back on the floor", s)
		}
	})

	t.Run("Up away from any rope does not climb", func(t *testing.T) {
		s := run(standing(50), sim.Input{Up: true}, g, 10)
		if s.Climbing || !s.OnGround {
			t.Fatalf("state %+v", s)
		}
	})
}

func TestStepIsPure(t *testing.T) {
	// The same inputs from the same state always give the same state, and
	// Step never changes its arguments.
	p := sim.DefaultParams()
	start := standing(200)
	in := sim.Input{Right: true, Jump: true}
	a := sim.Step(start, in, flat, p)
	b := sim.Step(start, in, flat, p)
	if a != b {
		t.Fatalf("two runs differ: %+v vs %+v", a, b)
	}
	if start != standing(200) {
		t.Fatal("Step changed its input state")
	}
}
