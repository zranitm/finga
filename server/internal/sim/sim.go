// Package sim is the deterministic movement simulation, mirrored by web/src/sim: no clock, randomness or map iteration.
package sim

import "math"

// NoPlatform is the Ground and DropPlatform value when no one-way platform is involved.
const NoPlatform = -1

// Params are the movement constants, tuned in game data.
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
	CastingSpeed float64 `json:"casting_speed"` // walking and climbing while casting, as a share of the speed
}

// SpeedFactor is 1 plus a move speed, rounded to thousandths so Go and TypeScript multiply by the same number.
func SpeedFactor(moveSpeed float64) float64 { return math.Round((1+moveSpeed)*1000) / 1000 }

// JumpPeak is how high a standing jump rises at p, stepped as Step steps it: a little under v²/2g.
func JumpPeak(p Params) float64 {
	g := Geometry{Solids: []Rect{{X: -1000, Y: 0, W: 2000, H: 32}}}
	s := State{Facing: 1, OnGround: true, Ground: NoPlatform, DropPlatform: NoPlatform}
	peak := 0.0
	for step := 0; step == 0 || !s.OnGround; step++ {
		s = Step(s, Input{Jump: step == 0}, g, p)
		peak = max(peak, -s.Y)
	}
	return peak
}

// Faster is p played f times as fast: walking, climbing, a jump's launch and the fall's clamp by f, and gravity by f², so a jump takes 1/f of the time; stepped at Dt, it peaks slightly lower as f rises (about 1.6 px at 1.3); the drop-through stays a time, and the casting speed a share.
func (p Params) Faster(f float64) Params {
	if f == 1 {
		return p
	}
	p.WalkSpeed = float64(p.WalkSpeed * f)
	p.ClimbSpeed = float64(p.ClimbSpeed * f)
	p.JumpVelocity = float64(p.JumpVelocity * f)
	p.MaxFallSpeed = float64(p.MaxFallSpeed * f)
	p.Gravity = float64(p.Gravity * float64(f*f))
	return p
}

// Rect is a half-open axis-aligned rectangle in pixels, y down, with its top-left corner at (X, Y).
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Geometry is a map's collision shapes in Tiled's order, platform indexes referring into Platforms, and the mobs the caller sets per step.
type Geometry struct {
	Solids    []Rect `json:"solids"`             // block movement on every side
	Platforms []Rect `json:"platforms"`          // one-way: only the top edge counts, only when landing
	Ropes     []Rect `json:"ropes"`              // climbable while the feet are within them
	Blockers  []Rect `json:"blockers,omitempty"` // mobs: a player's x overlap with one can't grow; the y move ignores them
}

// Input is the keys held during one sub-step.
type Input struct {
	Left  bool `json:"left,omitempty"`
	Right bool `json:"right,omitempty"`
	Up    bool `json:"up,omitempty"`
	Down  bool `json:"down,omitempty"`
	Jump  bool `json:"jump,omitempty"`
}

// State is a character's movement state, (X, Y) at the centre of its feet; Step returns a new one.
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
	Casting      int     `json:"casting"`       // sub-steps left of using a skill: walking and climbing at CastingSpeed, facing held
}

// Step advances s by one sub-step in web/src/sim's order; float64(...) on products stops arm64 fusing them into FMA.
func Step(s State, in Input, g Geometry, p Params) State {
	if s.Casting == 0 { // casting holds the facing
		switch {
		case in.Right:
			s.Facing = 1
		case in.Left:
			s.Facing = -1
		}
	}

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

	if s.Climbing {
		if in.Jump {
			s.Climbing = false
			s.VY = -p.JumpVelocity
			s.VX = walkVelocity(s, in, p)
		} else {
			s.VX, s.VY = 0, 0
			climb := pace(s, p.ClimbSpeed, p)
			switch {
			case in.Up && !in.Down:
				s.Y -= float64(climb * p.Dt)
			case in.Down && !in.Up:
				s.Y += float64(climb * p.Dt)
			}
			if !onAnyRope(s, g, p) {
				s.Climbing = false // past the top or the bottom: fall, or land on what is there
			}
			return tickTimer(s, p)
		}
	} else {
		s.VX = walkVelocity(s, in, p)

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

	s.VY += float64(p.Gravity * p.Dt)
	if s.VY > p.MaxFallSpeed {
		s.VY = p.MaxFallSpeed
	}

	from, fromX := box(s, p), s.X
	s.X += float64(s.VX * p.Dt)
	for _, solid := range g.Solids {
		if !box(s, p).Overlaps(solid) {
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
	for _, b := range g.Blockers {
		if !box(s, p).Overlaps(b) {
			continue
		}
		if x := Held(from, fromX, s.X, b); x != s.X {
			s.X, s.VX = x, 0
		}
	}

	prevFeet := s.Y
	s.Y += float64(s.VY * p.Dt)
	s.OnGround = false
	s.Ground = NoPlatform
	for _, solid := range g.Solids {
		if !box(s, p).Overlaps(solid) {
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

	return tickTimer(s, p)
}

func walkVelocity(s State, in Input, p Params) float64 {
	switch {
	case in.Right && !in.Left:
		return pace(s, p.WalkSpeed, p)
	case in.Left && !in.Right:
		return -pace(s, p.WalkSpeed, p)
	}
	return 0
}

// pace is a walking or climbing speed, at CastingSpeed while casting.
func pace(s State, speed float64, p Params) float64 {
	if s.Casting > 0 {
		return float64(speed * p.CastingSpeed)
	}
	return speed
}

// tickTimer counts the drop-through and the casting down by one sub-step.
func tickTimer(s State, p Params) State {
	s.DropTimer -= p.Dt
	if s.DropTimer <= 0 {
		s.DropTimer = 0
		s.DropPlatform = NoPlatform
	}
	if s.Casting > 0 {
		s.Casting--
	}
	return s
}

// canEnter reports whether in starts a climb: the feet are on rope, with rope left in the held direction.
func canEnter(s State, in Input, rope Rect, p Params) bool {
	if !feetOn(s, rope, p) {
		return false
	}
	return (in.Up && s.Y > rope.Y) || (in.Down && s.Y < rope.Y+rope.H)
}

// onAnyRope reports whether the feet are still within some rope.
func onAnyRope(s State, g Geometry, p Params) bool {
	for _, rope := range g.Ropes {
		if feetOn(s, rope, p) {
			return true
		}
	}
	return false
}

// feetOn: the feet are within r's vertical span (ends included) and the box overlaps r on X.
func feetOn(s State, r Rect, p Params) bool {
	return s.Y >= r.Y && s.Y <= r.Y+r.H && overlapsX(s, r, p)
}

func overlapsX(s State, r Rect, p Params) bool {
	return s.X-p.Width/2 < r.X+r.W && s.X+p.Width/2 > r.X
}

func box(s State, p Params) Rect {
	return Rect{X: s.X - p.Width/2, Y: s.Y - p.Height, W: p.Width, H: p.Height}
}

// Held is where a box from, at x fromX, moving to toX ends against b, so that its x overlap with b doesn't grow.
func Held(from Rect, fromX, toX float64, b Rect) float64 {
	switch {
	case toX > fromX && from.X < b.X && from.X+from.W < b.X+b.W:
		return min(toX, max(fromX, b.X-from.W/2))
	case toX < fromX && from.X+from.W > b.X+b.W && from.X > b.X:
		return max(toX, min(fromX, b.X+b.W+from.W/2))
	}
	return toX
}

// Overlaps is the half-open rectangle test: touching edges do not overlap.
func (a Rect) Overlaps(b Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
