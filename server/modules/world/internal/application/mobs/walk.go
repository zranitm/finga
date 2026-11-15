package mobs

import (
	"cmp"
	"math"
	"slices"
	"time"

	"finga/internal/kernel"
	"finga/internal/nav"
	"finga/internal/sim"
	"finga/modules/world/internal/model"
)

const (
	repathMoved = 1.0                    // metres a chased player moves before the mob finds a new path
	repathEvery = 500 * time.Millisecond // and the longest it keeps one
	repathSoon  = 100 * time.Millisecond // the soonest it finds another, when they've moved or its walk went nowhere
	spotTries   = 8                      // random points in its circle a mob tries for a spot that fits
	rings       = 4                      // contact rings round a player that chasing mobs take spots on
	driftSpeed  = 1.0                    // m/s at which idle mobs that overlap drift apart
)

// walk moves mb up to dist along its path, turning the way it goes, stopped by solids and players as the sim stops a body; a walk
// that goes nowhere drops the path and marks the mob stalled.
func (a *Arena) walk(mb *mob, dist float64) {
	mb.stalled = false
	for dist > 0 && len(mb.path) > 0 {
		next := mb.path[0]
		d := nav.Distance(mb.at(), next)
		if d == 0 {
			mb.path = mb.path[1:]
			continue
		}
		step := min(dist, d)
		ux, uz := (next.X-mb.x)/d, (next.Z-mb.z)/d
		mb.turnTo(model.YawOf(ux, uz))
		if a.move(mb, ux*step, uz*step, a.blockers) == 0 {
			mb.path, mb.stalled = nil, true
			return
		}
		dist -= step
		if nav.Distance(mb.at(), next) < 1e-9 {
			mb.path = mb.path[1:]
		}
	}
}

// move pushes mb (dx, dz) as the sim pushes a body, blockers in its way, and stands it on the ground there; it returns how far it
// went.
func (a *Arena) move(mb *mob, dx, dz float64, blockers []sim.Blocker) float64 {
	g := a.m.Geometry
	g.Blockers = blockers
	s := sim.Push(sim.State{X: mb.x, Y: mb.y, Z: mb.z, OnGround: true}, dx, dz, g, mb.params)
	moved := nav.Distance(mb.at(), nav.Point{X: s.X, Z: s.Z})
	if moved > 0 {
		mb.x, mb.z, mb.changed = s.X, s.Z, true
		mb.y = a.groundAt(s.X, s.Z, mb.y, mb.params.Step)
	}
	return moved
}

// pathTo is the way from mb to to inside its leash, ending on to itself; nil if there's none.
func (a *Arena) pathTo(mb *mob, to nav.Point, leash float64) []nav.Point {
	path := a.m.Grid().Path(mb.at(), to, mb.def.Radius, mb.home, leash)
	if len(path) > 0 {
		path[len(path)-1] = to
	}
	return path
}

// leash is how far from its circle's centre a mob follows anyone.
func (a *Arena) leash(mb *mob) float64 { return mb.homeR + a.data.MobRules().Leash }

// spotIn is a random point in mb's circle its body fits on, false if a few tries find none.
func (a *Arena) spotIn(mb *mob) (nav.Point, bool) {
	for range spotTries {
		turn, r := 2*math.Pi*a.roll.Float64(), mb.homeR*math.Sqrt(a.roll.Float64())
		sin, cos := math.Sincos(turn)
		p := nav.Point{X: mb.home.X + r*sin, Z: mb.home.Z + r*cos}
		if a.m.Grid().Fits(p, mb.def.Radius) {
			return p, true
		}
	}
	return nav.Point{}, false
}

// wander walks a mob to a random spot in its circle at its own pace, pauses a rolled while, and picks another.
func (a *Arena) wander(mb *mob) {
	if !mb.pathFor.IsZero() {
		mb.path, mb.pathFor = nil, kernel.CharacterID{}
	}
	if mb.pauseUntil > a.tick {
		return
	}
	if len(mb.path) == 0 {
		if to, ok := a.spotIn(mb); ok {
			mb.path = a.pathTo(mb, to, a.leash(mb))
		}
	}
	a.walk(mb, mb.speed()*a.interval.Seconds())
	if len(mb.path) == 0 {
		mb.pauseUntil = a.tick + a.ticks(a.pause())
	}
}

// pause is a wander's pause, rolled in the patrol's range.
func (a *Arena) pause() time.Duration {
	p := a.data.MobRules().Patrol
	return a.rolled(p.PauseMin, p.PauseMax)
}

// strayed reports whether mb is farther than its leash from its circle's edge, as a knock past it leaves one.
func (a *Arena) strayed(mb *mob) bool { return nav.Distance(mb.at(), mb.home) > a.leash(mb) }

// giveUp drops mb's target and anyone provoking it, and sends it home.
func (a *Arena) giveUp(mb *mob) {
	mb.target, mb.provokedUntil, mb.near = kernel.CharacterID{}, 0, false
	mb.path, mb.pathFor, mb.homing = nil, kernel.CharacterID{}, true
}

// goHome walks a mob that gave up back into its circle, blind to players until it's there.
func (a *Arena) goHome(mb *mob) {
	if nav.Distance(mb.at(), mb.home) <= mb.homeR {
		mb.homing, mb.path = false, nil
		return
	}
	if len(mb.path) == 0 {
		to, ok := a.spotIn(mb)
		if !ok {
			to = mb.home
		}
		if mb.path = a.pathTo(mb, to, nav.Distance(mb.at(), mb.home)+mb.homeR); len(mb.path) == 0 {
			mb.path = []nav.Point{to} // no way the grid knows of, as from a knock past a wall: straight, sliding along what's there
		}
	}
	a.walk(mb, mb.speed()*a.interval.Seconds())
	if mb.stalled {
		mb.pauseUntil = a.tick + a.ticks(repathEvery) // caught on something: it tries again in a while
	}
}

// chase walks a mob to its spot beside p, finding a new path when p has moved, its path is old, or its walk went nowhere; at its
// spot, it faces them. It gives up when p is past its leash.
func (a *Arena) chase(mb *mob, p Body) {
	if nav.Distance(p.at(), mb.home) > a.leash(mb) {
		a.giveUp(mb)
		a.goHome(mb)
		return
	}
	due := mb.pathFor != p.ID || a.tick >= mb.pathAt+a.ticks(repathEvery)
	soon := a.tick >= mb.pathAt+a.ticks(repathSoon) && (mb.stalled || nav.Distance(mb.pathTo, p.at()) > repathMoved)
	if due || soon {
		mb.path = a.pathTo(mb, a.spotBeside(mb, p), a.leash(mb))
		mb.pathFor, mb.pathAt, mb.pathTo, mb.stalled = p.ID, a.tick, p.at(), false
		mb.noWay = len(mb.path) == 0
	}
	if len(mb.path) == 0 {
		mb.face(p)
		return
	}
	a.walk(mb, mb.speed()*a.interval.Seconds())
	if len(mb.path) == 0 {
		mb.face(p)
	}
}

// slot is a spot taken on a player's rings, and the radius of the mob that took it.
type slot struct {
	at nav.Point
	r  float64
}

// spotBeside is mb's spot on p's contact rings (3d-world.md section 7): the mobs chasing p, in the order of their angle about p
// and then by id, each take the point nearest their own angle on the first ring with room, that fits and is inside their leash. A
// ring is a mob's radius plus p's out, and each next one a mob's width further. With no room on any, it's p's own point.
func (a *Arena) spotBeside(mb *mob, p Body) nav.Point {
	type chaser struct {
		mb    *mob
		angle float64
	}
	var chasers []chaser
	for _, other := range a.mobs {
		if other == mb || other.target == p.ID && other.attack < 0 && !other.homing {
			chasers = append(chasers, chaser{other, math.Atan2(other.x-p.X, other.z-p.Z)})
		}
	}
	slices.SortFunc(chasers, func(x, y chaser) int {
		return cmp.Or(cmp.Compare(x.angle, y.angle), cmp.Compare(x.mb.id, y.mb.id))
	})
	taken := make([]slot, 0, len(chasers))
	for _, c := range chasers {
		at := a.ringSpot(c.mb, p, c.angle, taken)
		if c.mb == mb {
			return at
		}
		taken = append(taken, slot{at, c.mb.def.Radius})
	}
	return p.at()
}

// ringSpot is the point nearest angle about p, ring by ring, clear of taken, that mb fits on inside its leash; p's point if none.
func (a *Arena) ringSpot(mb *mob, p Body, angle float64, taken []slot) nav.Point {
	r := mb.def.Radius
	for ring := range rings {
		reach := p.Radius + r + float64(ring)*2*r
		turn := r / reach // half a width along the ring
		for k := 0; float64(k)*turn <= math.Pi; k++ {
			for _, side := range []float64{1, -1} {
				sin, cos := math.Sincos(angle + side*float64(k)*turn)
				at := nav.Point{X: p.X + reach*sin, Z: p.Z + reach*cos}
				if a.free(at, r, taken) && nav.Distance(at, mb.home) <= a.leash(mb) && a.m.Grid().Fits(at, r) {
					return at
				}
				if k == 0 {
					break
				}
			}
		}
	}
	return p.at()
}

// free reports whether a mob of radius r at at would overlap none of taken.
func (a *Arena) free(at nav.Point, r float64, taken []slot) bool {
	for _, s := range taken {
		if nav.Distance(at, s.at) < r+s.r {
			return false
		}
	}
	return true
}

// idle reports whether mb stands still of its own accord: no attack, no stagger, nowhere to walk.
func (mb *mob) idle() bool {
	return mb.attack < 0 && mb.pose == model.Standing && len(mb.path) == 0
}

// spread drifts idle mobs that overlap apart, a little a tick and in id order, so a crowd that met on one spot ends side by side.
func (a *Arena) spread() {
	most := driftSpeed * a.interval.Seconds()
	for i, mb := range a.mobs {
		if !mb.idle() {
			continue
		}
		for _, other := range a.mobs[i+1:] {
			dx, dz := other.x-mb.x, other.z-mb.z
			d := nav.Distance(mb.at(), other.at())
			overlap := mb.def.Radius + other.def.Radius - d
			if !other.idle() || overlap <= 0 || other.y >= mb.y+mb.def.Height || mb.y >= other.y+other.def.Height {
				continue
			}
			if d == 0 {
				dx, dz, d = 1, 0, 1 // on one point: the later mob goes east
			}
			step := min(overlap/2, most)
			a.move(mb, -dx/d*step, -dz/d*step, a.blockers)
			a.move(other, dx/d*step, dz/d*step, a.blockers)
		}
	}
}
