// Package sim is the deterministic 3D movement simulation, mirrored op for op by web/src/sim: no trigonometry, clock, randomness or map iteration.
package sim

import "math"

// Diagonal is √½, a diagonal heading's share on each axis, written out so no trigonometry is needed.
const Diagonal = 0.7071067811865476

// Params are the movement constants in metres and seconds.
type Params struct {
	Dt           float64 `json:"dt"`             // one sub-step, 1/60 s
	WalkSpeed    float64 `json:"walk_speed"`     // m/s
	JumpVelocity float64 `json:"jump_velocity"`  // m/s, upward
	Gravity      float64 `json:"gravity"`        // m/s²
	MaxFallSpeed float64 `json:"max_fall_speed"` // m/s
	Radius       float64 `json:"radius"`         // the body's cylinder
	Height       float64 `json:"height"`
	Step         float64 `json:"step"`      // a solid's top up to this far above the feet is stepped onto
	MaxSlope     float64 `json:"max_slope"` // a rise per metre; a cell rising more is steep
	Snap         float64 `json:"snap"`      // the most the ground may fall away under a walking body before it falls
}

// SpeedFactor is 1 plus a move speed, rounded to thousandths so Go and TypeScript multiply by the same number.
func SpeedFactor(moveSpeed float64) float64 { return math.Round((1+moveSpeed)*1000) / 1000 }

// Faster is p played f times as fast: walking, a jump's launch and the fall's clamp by f, and gravity by f², so a jump takes 1/f of the time.
func (p Params) Faster(f float64) Params {
	if f == 1 {
		return p
	}
	p.WalkSpeed = float64(p.WalkSpeed * f)
	p.JumpVelocity = float64(p.JumpVelocity * f)
	p.MaxFallSpeed = float64(p.MaxFallSpeed * f)
	p.Gravity = float64(p.Gravity * float64(f*f))
	return p
}

// Box is an upright box up to Top, centred at (X, Z), HalfW along the unit axis (UX, UZ) and HalfD across it; axis-aligned is (1, 0).
type Box struct {
	X     float64 `json:"x"`
	Z     float64 `json:"z"`
	HalfW float64 `json:"half_w"`
	HalfD float64 `json:"half_d"`
	UX    float64 `json:"ux"`
	UZ    float64 `json:"uz"`
	Top   float64 `json:"top"`
}

// Cylinder is an upright cylinder up to Top.
type Cylinder struct {
	X      float64 `json:"x"`
	Z      float64 `json:"z"`
	Radius float64 `json:"radius"`
	Top    float64 `json:"top"`
}

// Blocker is a mob's circle from Bottom to Top: a body overlapping it may not move its centre closer.
type Blocker struct {
	X      float64 `json:"x"`
	Z      float64 `json:"z"`
	Radius float64 `json:"radius"`
	Bottom float64 `json:"bottom"`
	Top    float64 `json:"top"`
}

// Geometry is a map's ground and solids, SizeX by SizeZ metres with walls at the edges, and the mobs the caller sets per step.
type Geometry struct {
	SizeX     float64    `json:"size_x"`
	SizeZ     float64    `json:"size_z"`
	Terrain   Terrain    `json:"terrain"`
	Boxes     []Box      `json:"boxes"`
	Cylinders []Cylinder `json:"cylinders"`
	Blockers  []Blocker  `json:"blockers,omitempty"`
}

// Input is the keys held during one sub-step: up walks north (+z), right east (+x).
type Input struct {
	Left  bool `json:"left,omitempty"`
	Right bool `json:"right,omitempty"`
	Up    bool `json:"up,omitempty"`
	Down  bool `json:"down,omitempty"`
	Jump  bool `json:"jump,omitempty"`
}

// State is a body's movement state, (X, Y, Z) at the centre of its feet, y up, facing the unit heading (HX, HZ); Step returns a new one.
type State struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	VZ       float64 `json:"vz"`
	OnGround bool    `json:"on_ground"`
	HX       float64 `json:"hx"`
	HZ       float64 `json:"hz"`
}

// Step advances s by one sub-step in web/src/sim's order; float64(...) on products stops arm64 fusing them into FMA.
func Step(s State, in Input, g Geometry, p Params) State {
	x, z := keys(in.Right, in.Left), keys(in.Up, in.Down)
	if x != 0 && z != 0 {
		x, z = float64(x*Diagonal), float64(z*Diagonal)
	}
	if x != 0 || z != 0 {
		s.HX, s.HZ = x, z
	}
	s.VX, s.VZ = float64(x*p.WalkSpeed), float64(z*p.WalkSpeed)

	if s.OnGround && in.Jump {
		s.VY = p.JumpVelocity
		s.OnGround = false
	}

	s.VY -= float64(p.Gravity * p.Dt)
	if s.VY < -p.MaxFallSpeed {
		s.VY = -p.MaxFallSpeed
	}

	steep := false // moved into a cell steep along the way it moved
	if d := float64(s.VX * p.Dt); d != 0 {
		moved, into := slide(s, d, false, g, p)
		if moved != d {
			s.VX = 0
		}
		s.X += moved
		steep = into
	}
	if d := float64(s.VZ * p.Dt); d != 0 {
		moved, into := slide(s, d, true, g, p)
		if moved != d {
			s.VZ = 0
		}
		s.Z += moved
		steep = steep || into
	}

	walking, from := s.OnGround, s.Y
	s.Y += float64(s.VY * p.Dt)
	ground := Ground(s.X, s.Z, from+p.Step, g)
	switch {
	case s.Y <= ground && s.VY <= 0: // lands, or keeps walking up a slope
		s.Y, s.VY, s.OnGround = ground, 0, true
	case s.Y < ground: // rising past a lip: lifted onto it, still rising
		s.Y, s.OnGround = ground, false
	case walking && !steep && s.Y-ground <= p.Snap: // walks down a slope instead of hopping, but falls off a cliff
		s.Y, s.VY, s.OnGround = ground, 0, true
	default:
		s.OnGround = false
	}
	return s
}

// Push is s moved (dx, dz), x then z, stopped where a step would stop; only the server pushes, and the client takes the Delta.
func Push(s State, dx, dz float64, g Geometry, p Params) State {
	if dx != 0 {
		moved, _ := slide(s, dx, false, g, p)
		s.X += moved
	}
	if dz != 0 {
		moved, _ := slide(s, dz, true, g, p)
		s.Z += moved
	}
	return s
}

// keys is 1 for the first key alone, -1 for the second alone, else 0.
func keys(plus, minus bool) float64 {
	switch {
	case plus && !minus:
		return 1
	case minus && !plus:
		return -1
	}
	return 0
}

// slide is how far of d a body moves along x, or z when alongZ, and whether into a cell steep that way: edges, solids and mobs stop it at contact, terrain it can't climb is a wall.
func slide(s State, d float64, alongZ bool, g Geometry, p Params) (float64, bool) {
	at, across, size := s.X, s.Z, g.SizeX
	if alongZ {
		at, across, size = s.Z, s.X, g.SizeZ
	}
	d = limit(d, math.Inf(-1), p.Radius-at)
	d = limit(d, size-p.Radius-at, math.Inf(1))
	for _, b := range g.Boxes {
		if b.Top > s.Y+p.Step {
			if lo, hi, ok := boxSpan(b, s.X, s.Z, alongZ, p.Radius); ok {
				d = limit(d, lo, hi)
			}
		}
	}
	for _, c := range g.Cylinders {
		if c.Top > s.Y+p.Step {
			if lo, hi, ok := circleSpan(c.X, c.Z, c.Radius+p.Radius, s.X, s.Z, alongZ); ok {
				d = limit(d, lo, hi)
			}
		}
	}
	for _, b := range g.Blockers {
		if s.Y < b.Top && s.Y+p.Height > b.Bottom {
			if lo, hi, ok := circleSpan(b.X, b.Z, b.Radius+p.Radius, s.X, s.Z, alongZ); ok {
				d = limit(d, lo, hi)
			}
		}
	}
	if d == 0 {
		return 0, false
	}
	x, z := at+d, across
	if alongZ {
		x, z = across, at+d
	}
	steep, h := g.Terrain.Steep(x, z, alongZ), g.Terrain.Height(x, z)
	switch {
	case s.OnGround: // uphill into a steep cell
		if steep && h > s.Y {
			return 0, false
		}
	case steep && h > g.Terrain.Height(s.X, s.Z): // in the air, uphill, a steep cell is a cliff as high as its highest sample
		if s.Y < g.Terrain.Top(x, z) {
			return 0, false
		}
	case h > s.Y:
		return 0, false
	}
	return d, steep
}

// limit cuts a move of d against the open span (lo, hi) of moves that overlap: it stops at the span's edge, and from inside only goes no deeper.
func limit(d, lo, hi float64) float64 {
	switch {
	case lo >= 0:
		if d > lo {
			return lo
		}
		return d
	case hi <= 0:
		if d < hi {
			return hi
		}
		return d
	}
	if depth(d, lo, hi) > depth(0, lo, hi) {
		return 0
	}
	return d
}

// depth is how far a move of d lies inside the span (lo, hi), negative outside.
func depth(d, lo, hi float64) float64 {
	if d-lo < hi-d {
		return d - lo
	}
	return hi - d
}

// circleSpan is the open span of moves along x, or z when alongZ, from (x, z) that bring it within reach of (cx, cz).
func circleSpan(cx, cz, reach, x, z float64, alongZ bool) (lo, hi float64, ok bool) {
	along, across := x-cx, z-cz
	if alongZ {
		along, across = z-cz, x-cx
	}
	room := float64(reach*reach) - float64(across*across)
	if room <= 0 {
		return 0, 0, false
	}
	half := math.Sqrt(room)
	return -along - half, -along + half, true
}

// boxSpan is the open span of moves along x, or z when alongZ, from (x, z) that bring a circle of radius r into b: in b's frame, two slabs and four corner discs.
func boxSpan(b Box, x, z float64, alongZ bool, r float64) (lo, hi float64, ok bool) {
	dx, dz := x-b.X, z-b.Z
	u := float64(dx*b.UX) + float64(dz*b.UZ)
	v := float64(dz*b.UX) - float64(dx*b.UZ)
	du, dv := b.UX, -b.UZ
	if alongZ {
		du, dv = b.UZ, b.UX
	}
	lo, hi = math.Inf(1), math.Inf(-1)
	add := func(l, h float64, in bool) {
		if in {
			if l < lo {
				lo = l
			}
			if h > hi {
				hi = h
			}
		}
	}
	add(rectSpan(u, v, du, dv, b.HalfW+r, b.HalfD))
	add(rectSpan(u, v, du, dv, b.HalfW, b.HalfD+r))
	for _, corner := range [4][2]float64{{b.HalfW, b.HalfD}, {-b.HalfW, b.HalfD}, {-b.HalfW, -b.HalfD}, {b.HalfW, -b.HalfD}} {
		add(discSpan(u-corner[0], v-corner[1], du, dv, r))
	}
	return lo, hi, lo < hi
}

// rectSpan is the open span of s for which (u, v) + s(du, dv) lies inside |u| < hu, |v| < hv.
func rectSpan(u, v, du, dv, hu, hv float64) (lo, hi float64, ok bool) {
	lo, hi = math.Inf(-1), math.Inf(1)
	for _, axis := range [2][3]float64{{u, du, hu}, {v, dv, hv}} {
		at, rate, half := axis[0], axis[1], axis[2]
		if rate == 0 {
			if math.Abs(at) >= half {
				return 0, 0, false
			}
			continue
		}
		a, b := (-half-at)/rate, (half-at)/rate
		if a > b {
			a, b = b, a
		}
		if a > lo {
			lo = a
		}
		if b < hi {
			hi = b
		}
	}
	return lo, hi, lo < hi
}

// discSpan is the open span of s for which (pu, pv) + s(du, dv) lies within r of the origin; a line passing at exactly r misses.
func discSpan(pu, pv, du, dv, r float64) (lo, hi float64, ok bool) {
	a := float64(du*du) + float64(dv*dv)
	cross := float64(pu*dv) - float64(pv*du)
	room := float64(float64(r*r)*a) - float64(cross*cross)
	if room <= 0 {
		return 0, 0, false
	}
	mid := -(float64(pu*du) + float64(pv*dv)) / a
	half := math.Sqrt(room) / a
	return mid - half, mid + half, true
}

// Ground is the ground under (x, z): the terrain, or the top of a solid holding the point that is no higher than below.
func Ground(x, z, below float64, g Geometry) float64 {
	ground := g.Terrain.Height(x, z)
	for _, b := range g.Boxes {
		if b.Top > ground && b.Top <= below && b.Holds(x, z) {
			ground = b.Top
		}
	}
	for _, c := range g.Cylinders {
		if c.Top > ground && c.Top <= below && c.Holds(x, z) {
			ground = c.Top
		}
	}
	return ground
}

// Holds reports whether the box's footprint holds (x, z), its edges included.
func (b Box) Holds(x, z float64) bool {
	dx, dz := x-b.X, z-b.Z
	u := float64(dx*b.UX) + float64(dz*b.UZ)
	v := float64(dz*b.UX) - float64(dx*b.UZ)
	return math.Abs(u) <= b.HalfW && math.Abs(v) <= b.HalfD
}

// Holds reports whether the cylinder's footprint holds (x, z), its edge included.
func (c Cylinder) Holds(x, z float64) bool {
	dx, dz := x-c.X, z-c.Z
	return float64(dx*dx)+float64(dz*dz) <= float64(c.Radius*c.Radius)
}
