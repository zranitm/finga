// Package sim is the deterministic movement simulation (Low-level design,
// page 5). The server runs it from inputs; the client runs the same
// algorithm in TypeScript (web/src/sim) to predict its own character. The
// shared vectors in testdata/movement must pass in both, so everything here
// is float64, the order of operations inside Step is fixed and documented,
// and nothing depends on a clock, randomness, or map iteration.
//
// Coordinates are pixels with y growing downward, as in Tiled. A character
// is a Width × Height box whose position (X, Y) is the centre of its feet:
// the box spans [X-Width/2, X+Width/2) by [Y-Height, Y). Rectangles are
// half-open, so a box flush against an edge does not touch it.
package sim

// NoPlatform is the Ground and DropPlatform value when no one-way platform
// is involved.
const NoPlatform = -1

// Params are the movement constants. Starting values on page 5; they live
// in game data so they can be tuned without code changes.
type Params struct {
	Dt           float64 `json:"dt"`             // one sub-step, 1/60 s
	WalkSpeed    float64 `json:"walk_speed"`     // px/s
	JumpVelocity float64 `json:"jump_velocity"`  // px/s, upward
	Gravity      float64 `json:"gravity"`        // px/s²
	MaxFallSpeed float64 `json:"max_fall_speed"` // px/s
	ClimbSpeed   float64 `json:"climb_speed"`    // px/s
	DropThrough  float64 `json:"drop_through"`   // seconds a dropped-through platform is ignored
	Width        float64 `json:"width"`          // character box
	Height       float64 `json:"height"`
}

// DefaultParams are page 5's starting constants.
func DefaultParams() Params {
	return Params{
		Dt:           1.0 / 60,
		WalkSpeed:    200,
		JumpVelocity: 620,
		Gravity:      1900,
		MaxFallSpeed: 900,
		ClimbSpeed:   140,
		DropThrough:  0.25,
		Width:        24,
		Height:       56,
	}
}

// Rect is an axis-aligned rectangle with its top-left corner at (X, Y).
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Geometry is what a map's object layers give the simulation. Slices keep
// Tiled's order, and platform indexes refer into Platforms.
type Geometry struct {
	Solids    []Rect `json:"solids"`    // block movement on every side
	Platforms []Rect `json:"platforms"` // one-way: only the top edge counts, only when landing
	Ropes     []Rect `json:"ropes"`     // climbable while the feet are within them
}

// Input is the keys held during one sub-step.
type Input struct {
	Left  bool `json:"left,omitempty"`
	Right bool `json:"right,omitempty"`
	Up    bool `json:"up,omitempty"`
	Down  bool `json:"down,omitempty"`
	Jump  bool `json:"jump,omitempty"`
}

// State is a character's movement state. It is a plain value: Step returns
// a new one and never changes its argument.
type State struct {
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	VX           float64 `json:"vx"`
	VY           float64 `json:"vy"`
	OnGround     bool    `json:"on_ground"`
	Climbing     bool    `json:"climbing"`
	Facing       int     `json:"facing"`        // 1 right, -1 left
	Ground       int     `json:"ground"`        // platform index stood on, NoPlatform on a solid or in the air
	DropPlatform int     `json:"drop_platform"` // platform ignored while DropTimer > 0
	DropTimer    float64 `json:"drop_timer"`    // seconds left of the drop-through
}

// Step advances s by one sub-step of p.Dt under input in, in this order:
//
//  1. Facing follows Right or Left (Right wins when both are held).
//  2. Rope entry: Up with the feet inside a rope's span and rope above
//     them, or Down with rope below them, starts climbing, snapped to the
//     rope's centre, with velocity cleared. The feet decide, not the box,
//     so a character standing at a rope's end does not re-enter it.
//  3. Climbing: Jump leaves the rope with a full jump (horizontal speed from
//     Left/Right); otherwise the character moves at ClimbSpeed up or down,
//     and leaves the rope once the feet pass either end. No gravity and no
//     collision while climbing; the step ends after the timer (9).
//  4. Walking: VX is WalkSpeed to the held side, 0 when neither or both.
//  5. Jump from the ground: with Down while on a one-way platform it drops
//     through (the platform is ignored for DropThrough seconds); otherwise
//     VY becomes -JumpVelocity.
//  6. Gravity: VY grows by Gravity·Dt, clamped at MaxFallSpeed.
//  7. Move on X by VX·Dt and push out of any solid, zeroing VX.
//  8. Move on Y by VY·Dt. OnGround is cleared first. Moving down, a solid
//     or a one-way platform whose top the feet were at or above before the
//     move stops the fall: feet on its top, VY 0, OnGround set. Moving up, a
//     solid stops the rise: head on its underside, VY 0. Solids are checked
//     before platforms, each in slice order; the ignored platform is skipped.
//  9. The drop-through timer counts down and releases its platform at 0.
func Step(s State, in Input, g Geometry, p Params) State {
	// 1. Facing.
	switch {
	case in.Right:
		s.Facing = 1
	case in.Left:
		s.Facing = -1
	}

	// 2. Rope entry.
	if !s.Climbing && (in.Up || in.Down) {
		for _, rope := range g.Ropes {
			if canEnter(s, in, rope, p) {
				s.Climbing = true
				s.X = rope.X + rope.W/2
				s.VX, s.VY = 0, 0
				s.OnGround = false
				s.Ground = NoPlatform
				break
			}
		}
	}

	// 3. Climbing.
	if s.Climbing {
		if in.Jump {
			s.Climbing = false
			s.VY = -p.JumpVelocity
			s.VX = walkVelocity(in, p)
		} else {
			s.VX, s.VY = 0, 0
			switch {
			case in.Up && !in.Down:
				s.Y -= p.ClimbSpeed * p.Dt
			case in.Down && !in.Up:
				s.Y += p.ClimbSpeed * p.Dt
			}
			if !onAnyRope(s, g, p) {
				s.Climbing = false // past the top or the bottom: fall, or land on what is there
			}
			return tickTimer(s, p)
		}
	} else {
		// 4. Walking.
		s.VX = walkVelocity(in, p)

		// 5. Jump or drop through.
		if s.OnGround && in.Jump {
			if in.Down && s.Ground != NoPlatform {
				s.DropPlatform = s.Ground
				s.DropTimer = p.DropThrough
			} else {
				s.VY = -p.JumpVelocity
			}
			s.OnGround = false
			s.Ground = NoPlatform
		}
	}

	// 6. Gravity.
	s.VY += p.Gravity * p.Dt
	if s.VY > p.MaxFallSpeed {
		s.VY = p.MaxFallSpeed
	}

	// 7. X.
	s.X += s.VX * p.Dt
	for _, solid := range g.Solids {
		if !overlaps(box(s, p), solid) {
			continue
		}
		switch {
		case s.VX > 0:
			s.X = solid.X - p.Width/2
		case s.VX < 0:
			s.X = solid.X + solid.W + p.Width/2
		}
		s.VX = 0
	}

	// 8. Y.
	prevFeet := s.Y
	s.Y += s.VY * p.Dt
	s.OnGround = false
	s.Ground = NoPlatform
	for _, solid := range g.Solids {
		if !overlaps(box(s, p), solid) {
			continue
		}
		switch {
		case s.VY > 0:
			s.Y = solid.Y
			s.OnGround = true
		case s.VY < 0:
			s.Y = solid.Y + solid.H + p.Height
		}
		s.VY = 0
	}
	if s.VY > 0 {
		for i, plat := range g.Platforms {
			if i == s.DropPlatform || prevFeet > plat.Y || s.Y < plat.Y {
				continue
			}
			if overlapsX(s, plat, p) {
				s.Y = plat.Y
				s.VY = 0
				s.OnGround = true
				s.Ground = i
				break
			}
		}
	}

	// 9. Drop-through timer.
	return tickTimer(s, p)
}

func walkVelocity(in Input, p Params) float64 {
	switch {
	case in.Right && !in.Left:
		return p.WalkSpeed
	case in.Left && !in.Right:
		return -p.WalkSpeed
	}
	return 0
}

func tickTimer(s State, p Params) State {
	s.DropTimer -= p.Dt
	if s.DropTimer <= 0 {
		s.DropTimer = 0
		s.DropPlatform = NoPlatform
	}
	return s
}

// canEnter reports whether in starts a climb on rope: the feet are within
// the rope's span and the box overlaps it on X, and there is rope left in
// the held direction (above the feet for Up, below for Down).
func canEnter(s State, in Input, rope Rect, p Params) bool {
	if !feetOn(s, rope, p) {
		return false
	}
	return (in.Up && s.Y > rope.Y) || (in.Down && s.Y < rope.Y+rope.H)
}

// onAnyRope reports whether the feet are still within some rope while
// climbing (the X was snapped to the rope on entry).
func onAnyRope(s State, g Geometry, p Params) bool {
	for _, rope := range g.Ropes {
		if feetOn(s, rope, p) {
			return true
		}
	}
	return false
}

// feetOn: the feet are within r's vertical span (ends included) and the box
// overlaps r on X.
func feetOn(s State, r Rect, p Params) bool {
	return s.Y >= r.Y && s.Y <= r.Y+r.H && overlapsX(s, r, p)
}

func overlapsX(s State, r Rect, p Params) bool {
	return s.X-p.Width/2 < r.X+r.W && s.X+p.Width/2 > r.X
}

func box(s State, p Params) Rect {
	return Rect{X: s.X - p.Width/2, Y: s.Y - p.Height, W: p.Width, H: p.Height}
}

// overlaps is the half-open rectangle test: touching edges do not overlap.
func overlaps(a, b Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
