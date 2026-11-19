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
	Sprint       Sprint  `json:"sprint,omitzero"`
}

// Sprint is how walking out of combat breaks into a sprint (large-regions.md section 7): after After seconds of it, the walk runs Speed times as fast; the zero Sprint never sprints, as in combat.
type Sprint struct {
	After float64 `json:"after"` // s
	Speed float64 `json:"speed"` // × the walk speed
}

// Fighting is p as a body in combat moves: it never sprints.
func (p Params) Fighting() Params {
	p.Sprint = Sprint{}
	return p
}

// SprintSteps is how many sub-steps of walking break into a sprint, 0 for none.
func SprintSteps(p Params) int {
	if p.Sprint.After <= 0 {
		return 0
	}
	return int(math.Round(p.Sprint.After / p.Dt))
}

// EndSprint is s with its sprint's count ended, as an attack or a skill pressed ends it.
func EndSprint(s State) State {
	s.Sprint = 0
	return s
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
	solids    *solids    // its boxes and cylinders bucketed, set by Indexed; nil scans every one
	blockers  *Blockers  // its blockers bucketed, set by Blocking
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
	Move     Move    `json:"move,omitzero"`
	Held     int     `json:"held,omitzero"` // sub-steps left of a status holding the body: no keys, no jump, no skill's move, gravity still running
	Slow     Slow    `json:"slow,omitzero"`
	Sprint   int     `json:"sprint,omitzero"` // sub-steps walked toward a sprint, held at SprintSteps once there: from the next, it sprints
}

// Slow is a status slowing the walk: Factor × the walk speed for Left more sub-steps; a skill's move keeps its speed.
type Slow struct {
	Left   int     `json:"left"`
	Factor float64 `json:"factor"`
}

// Move is a skill's dash or leap under way: (VX, VZ) replaces the walk for Left more sub-steps. With Stop it ends at a mob's body; with Through it passes mobs, and if it runs out inside one it ends at (ClearX, ClearZ), the farthest point it reached that no mob's body held.
type Move struct {
	VX      float64 `json:"vx"`
	VZ      float64 `json:"vz"`
	Left    int     `json:"left"`
	Stop    bool    `json:"stop"`
	Through bool    `json:"through,omitzero"`
	ClearX  float64 `json:"clear_x,omitzero"`
	ClearZ  float64 `json:"clear_z,omitzero"`
}

// Way is a skill's move: along the unit (X, Z) for Length metres.
type Way struct {
	X      float64
	Z      float64
	Length float64
}

// Dash starts a skill's dash along w over n sub-steps, stopping at a mob's body or passing through mobs as m says; a leap also hops, so as to land on flat ground as it ends.
func Dash(s State, w Way, n int, m Mobs, leap bool, p Params) State {
	if n < 1 {
		return s
	}
	speed := w.Length / float64(float64(n)*p.Dt)
	s.Move = Move{VX: float64(w.X * speed), VZ: float64(w.Z * speed), Left: n, Stop: m == StopAtMob}
	if m == ThroughMobs {
		s.Move.Through, s.Move.ClearX, s.Move.ClearZ = true, s.X, s.Z
	}
	if leap {
		s.VY = float64(float64(p.Gravity*p.Dt)*float64(n+1)) / 2
		s.OnGround = false
	}
	return s
}

// Mobs is how a dash or leap meets mobs' bodies.
type Mobs int

// The ways a move meets mobs.
const (
	SlideOnMobs Mobs = iota // stopped by a body along the way it pushes, sliding along it
	StopAtMob               // ended at the first body it meets
	ThroughMobs             // passing through them, to end clear of every body
)

// blinkStep is how far apart a blink tests its way.
const blinkStep = 0.25

// Blink moves s at once along w, as far as it can: tested every blinkStep, it stops short of a solid, an uphill steep cell or the map's edge, passes through mobs, and ends at the farthest point tested that no mob's body holds.
func Blink(s State, w Way, g Geometry, p Params) State {
	walls := g
	walls.Blockers = nil
	at, end := s, s
	for gone := 0.0; gone < w.Length; {
		d := w.Length - gone
		if d > blinkStep {
			d = blinkStep
		}
		next := at
		if dx := float64(w.X * d); dx != 0 {
			if moved, _, _, _ := slide(next, dx, false, walls, p); moved != dx {
				break
			}
			next.X += dx
		}
		if dz := float64(w.Z * d); dz != 0 {
			if moved, _, _, _ := slide(next, dz, true, walls, p); moved != dz {
				break
			}
			next.Z += dz
		}
		at, gone = next, gone+d
		if !inMob(at, g, p) {
			end = at
		}
	}
	return end
}

// inMob is whether a mob's body holds s's.
func inMob(s State, g Geometry, p Params) bool {
	blockers := g.nearBlockers(s.X-p.Radius, s.Z-p.Radius, s.X+p.Radius, s.Z+p.Radius)
	for i, ok := blockers.next(); ok; i, ok = blockers.next() {
		if b := g.Blockers[i]; s.Y < b.Top && s.Y+p.Height > b.Bottom {
			dx, dz, r := s.X-b.X, s.Z-b.Z, b.Radius+p.Radius
			if float64(dx*dx)+float64(dz*dz) < float64(r*r) {
				return true
			}
		}
	}
	return false
}

// Step advances s by one sub-step in web/src/sim's order; float64(...) on products stops arm64 fusing them into FMA.
func Step(s State, in Input, g Geometry, p Params) State {
	if s.Held > 0 { // held where it is, its heading kept, a skill's move ended
		in, s.Move = Input{}, Move{}
	}
	x, z := keys(in.Right, in.Left), keys(in.Up, in.Down)
	if x != 0 && z != 0 {
		x, z = float64(x*Diagonal), float64(z*Diagonal)
	}
	if x != 0 || z != 0 {
		s.HX, s.HZ = x, z
	}
	moving := s.Move.Left > 0
	walking, sprintAt := (x != 0 || z != 0) && !moving, SprintSteps(p)
	sprinting := walking && sprintAt > 0 && s.Sprint >= sprintAt
	switch {
	case !walking || sprintAt == 0: // standing, a skill's move, a hold or a fight ends the count; a jump keeps it
		s.Sprint = 0
	case s.Sprint < sprintAt:
		s.Sprint++
	}
	speed := p.WalkSpeed
	if sprinting {
		speed = float64(speed * p.Sprint.Speed)
	}
	if s.Slow.Left > 0 {
		speed = float64(speed * s.Slow.Factor)
	}
	s.VX, s.VZ = float64(x*speed), float64(z*speed)
	if moving { // a skill's move replaces the walk
		s.VX, s.VZ = s.Move.VX, s.Move.VZ
	}

	if s.OnGround && in.Jump {
		s.VY = p.JumpVelocity
		s.OnGround = false
	}

	s.VY -= float64(p.Gravity * p.Dt)
	if s.VY < -p.MaxFallSpeed {
		s.VY = -p.MaxFallSpeed
	}

	steep := false       // moved into a cell steep along the way it moved
	cut, cyl := true, -1 // whether every key's move was cut short, and the cylinder that cut one
	atMob := false       // whether a mob's body cut a move short
	solids := g          // what the body slides against: no mobs for a move passing through them
	if moving && s.Move.Through {
		solids.Blockers = nil
	}
	dx, dz := float64(s.VX*p.Dt), float64(s.VZ*p.Dt)
	if dx != 0 {
		moved, into, by, mob := slide(s, dx, false, solids, p)
		s.X += moved
		steep, atMob = into, mob
		if moved == dx {
			cut = false
		} else {
			s.VX = 0
			if cyl < 0 {
				cyl = by
			}
		}
	}
	if dz != 0 {
		moved, into, by, mob := slide(s, dz, true, solids, p)
		s.Z += moved
		steep, atMob = steep || into, atMob || mob
		if moved == dz {
			cut = false
		} else {
			s.VZ = 0
			if cyl < 0 {
				cyl = by
			}
		}
	}
	if cut && cyl >= 0 {
		var into bool
		s, into = round(s, g.Cylinders[cyl], dx, dz, solids, p)
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
	if moving {
		inside := s.Move.Through && inMob(s, g, p)
		if s.Move.Through && !inside {
			s.Move.ClearX, s.Move.ClearZ = s.X, s.Z
		}
		if s.Move.Left--; s.Move.Left == 0 || s.Move.Stop && atMob {
			if inside { // run out inside a mob: back to the farthest point clear of them, as a blink ends
				s.X, s.Z = s.Move.ClearX, s.Move.ClearZ
			}
			s.Move = Move{}
		}
	}
	if s.Held > 0 {
		s.Held--
	}
	if s.Slow.Left > 1 {
		s.Slow.Left--
	} else {
		s.Slow = Slow{}
	}
	return s
}

// Push is s moved (dx, dz), x then z, stopped where a step would stop; only the server pushes, and the client takes the Delta.
func Push(s State, dx, dz float64, g Geometry, p Params) State {
	if dx != 0 {
		moved, _, _, _ := slide(s, dx, false, g, p)
		s.X += moved
	}
	if dz != 0 {
		moved, _, _, _ := slide(s, dz, true, g, p)
		s.Z += moved
	}
	return s
}

// rimClear is how far past touching a body rounding onto a rim's point straight north, south, east or west of a cylinder stands, so rounding error can't leave it a sliver inside, stopping its next move.
const rimClear = 1e-6

// round is s, stopped at the rim of c, moved round it by the share of (dx, dz) along its tangent, at its distance from c's centre, and no farther than the rim's next point straight north, south, east or west of c; x and z each go as far as a step lets them, the one leaving c first. It returns whether into a steep cell.
func round(s State, c Cylinder, dx, dz float64, g Geometry, p Params) (State, bool) {
	nx, nz := s.X-c.X, s.Z-c.Z
	r := math.Sqrt(float64(nx*nx) + float64(nz*nz))
	if r == 0 {
		return s, false
	}
	tx, tz := -nz/r, nx/r
	along := float64(dx*tx) + float64(dz*tz)
	if along == 0 { // pressing straight at the centre
		return s, false
	}
	ox, oz := nx+float64(along*tx), nz+float64(along*tz)
	k := r / math.Sqrt(float64(ox*ox)+float64(oz*oz))
	ox, oz = float64(ox*k), float64(oz*k)
	out := max(r, c.Radius+p.Radius) + rimClear
	switch { // a step past the rim's point straight north, south, east or west of c stops on it, just clear: its legs, x then z or z then x, would cut into c
	case nx < 0 && ox > 0 || nx > 0 && ox < 0:
		ox, oz = 0, out
		if nz < 0 {
			oz = -out
		}
	case nz < 0 && oz > 0 || nz > 0 && oz < 0:
		ox, oz = out, 0
		if nx < 0 {
			ox = -out
		}
	}
	mx, mz := c.X+ox-s.X, c.Z+oz-s.Z
	steep := false
	move := func(alongZ bool) {
		d := mx
		if alongZ {
			d = mz
		}
		if d == 0 {
			return
		}
		moved, into, _, _ := slide(s, d, alongZ, g, p)
		if alongZ {
			s.Z += moved
		} else {
			s.X += moved
		}
		steep = steep || into
	}
	xFirst := math.Abs(ox) >= math.Abs(nx)
	move(!xFirst)
	move(xFirst)
	return s, steep
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

// slide is how far of d a body moves along x, or z when alongZ, whether into a cell steep that way, the cylinder that cut it short, else -1, and whether a mob did: edges, solids and mobs stop it at contact, terrain it can't climb is a wall.
func slide(s State, d float64, alongZ bool, g Geometry, p Params) (float64, bool, int, bool) {
	at, across, size := s.X, s.Z, g.SizeX
	if alongZ {
		at, across, size = s.Z, s.X, g.SizeZ
	}
	d = limit(d, math.Inf(-1), p.Radius-at)
	d = limit(d, size-p.Radius-at, math.Inf(1))
	x0, z0, x1, z1 := s.X-p.Radius, s.Z-p.Radius, s.X+p.Radius, s.Z+p.Radius // the swept body's bounds
	switch {
	case alongZ && d < 0:
		z0 += d
	case alongZ:
		z1 += d
	case d < 0:
		x0 += d
	default:
		x1 += d
	}
	boxes := g.near(false, x0, z0, x1, z1)
	for i, ok := boxes.next(); ok; i, ok = boxes.next() {
		if b := g.Boxes[i]; b.Top > s.Y+p.Step {
			if lo, hi, ok := boxSpan(b, s.X, s.Z, alongZ, p.Radius); ok {
				d = limit(d, lo, hi)
			}
		}
	}
	by, mob := -1, false
	cylinders := g.near(true, x0, z0, x1, z1)
	for i, ok := cylinders.next(); ok; i, ok = cylinders.next() {
		if c := g.Cylinders[i]; c.Top > s.Y+p.Step {
			if lo, hi, ok := circleSpan(c.X, c.Z, c.Radius+p.Radius, s.X, s.Z, alongZ); ok {
				if cut := limit(d, lo, hi); cut != d {
					d, by = cut, i
				}
			}
		}
	}
	blockers := g.nearBlockers(x0, z0, x1, z1)
	for i, ok := blockers.next(); ok; i, ok = blockers.next() {
		if b := g.Blockers[i]; s.Y < b.Top && s.Y+p.Height > b.Bottom {
			if lo, hi, ok := circleSpan(b.X, b.Z, b.Radius+p.Radius, s.X, s.Z, alongZ); ok {
				if cut := limit(d, lo, hi); cut != d {
					d, by, mob = cut, -1, true
				}
			}
		}
	}
	if d == 0 {
		return 0, false, by, mob
	}
	x, z := at+d, across
	if alongZ {
		x, z = across, at+d
	}
	steep, h := g.Terrain.Steep(x, z, alongZ), g.Terrain.Height(x, z)
	switch {
	case s.OnGround: // uphill into a steep cell
		if steep && h > s.Y {
			return 0, false, -1, mob
		}
	case steep && h > g.Terrain.Height(s.X, s.Z): // in the air, uphill, a steep cell is a cliff as high as its highest sample
		if s.Y < g.Terrain.Top(x, z) {
			return 0, false, -1, mob
		}
	case h > s.Y:
		return 0, false, -1, mob
	}
	return d, steep, by, mob
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
	boxes := g.near(false, x, z, x, z)
	for i, ok := boxes.next(); ok; i, ok = boxes.next() {
		if b := g.Boxes[i]; b.Top > ground && b.Top <= below && b.Holds(x, z) {
			ground = b.Top
		}
	}
	cylinders := g.near(true, x, z, x, z)
	for i, ok := cylinders.next(); ok; i, ok = cylinders.next() {
		if c := g.Cylinders[i]; c.Top > ground && c.Top <= below && c.Holds(x, z) {
			ground = c.Top
		}
	}
	return ground
}

// SolidOver reports whether a solid whose top is over y holds (x, z).
func (g Geometry) SolidOver(x, z, y float64) bool {
	boxes := g.near(false, x, z, x, z)
	for i, ok := boxes.next(); ok; i, ok = boxes.next() {
		if b := g.Boxes[i]; b.Top > y && b.Holds(x, z) {
			return true
		}
	}
	cylinders := g.near(true, x, z, x, z)
	for i, ok := cylinders.next(); ok; i, ok = cylinders.next() {
		if c := g.Cylinders[i]; c.Top > y && c.Holds(x, z) {
			return true
		}
	}
	return false
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
