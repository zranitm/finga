package mobs

import (
	"cmp"
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

const (
	repathMoved = 1.0                    // metres a chased player moves before the mob looks for a new way
	repathEvery = 500 * time.Millisecond // the longest a chaser keeps a path
	repathSoon  = 100 * time.Millisecond // the soonest a mob looks for another
	searches    = 4                      // path searches an arena runs a tick at most; the rest wait their turn
	stallShare  = 0.25                   // a walk making under this share of its step is stalled
	avoidFor    = 2 * time.Second        // how long a stalled mob routes round the cell it couldn't enter
	stuckFor    = 3 * time.Second        // a mob getting nowhere this long gives up what it's walking to
	arrived     = 0.25                   // metres from its spot a chaser counts as there
	contactGap  = 0.05                   // metres a ring spot keeps clear of the player's body
	touchStep   = math.Pi / 36           // 5°: how far round a player each try for a mob's way out turns
	touchTries  = 37                     // tries each way, round to the far side
	spotTries   = 8                      // random points in its circle a mob tries for a spot that fits
	rings       = 5                      // rings round a player that chasing mobs take spots on: the contact ring, one at their reach's edge, and outer ones
	touching    = 1e-3                   // metres between mobs side by side on a ring
	driftSpeed  = 1.0                    // m/s at which mobs that overlap drift apart
)

// walk moves mb up to dist along its path, turning the way it goes; a step it makes under stallShare of drops the path.
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
		if a.move(mb, float64(ux*step), float64(uz*step), &a.blocking) < stallShare*step {
			mb.path, mb.stalled, mb.avoid, mb.avoidUntil = nil, true, next, a.tick+a.ticks(avoidFor)
			return
		}
		mb.movedAt = a.tick
		dist -= step
		if nav.Distance(mb.at(), next) < 1e-9 {
			mb.path = mb.path[1:]
		}
	}
}

// move pushes mb (dx, dz) as the sim pushes a body, stopped by blockers, onto the ground there; it returns how far it went.
func (a *Arena) move(mb *mob, dx, dz float64, blockers *sim.Blockers) float64 {
	g := a.m.Geometry.Blocking(blockers)
	s := sim.Push(sim.State{X: mb.x, Y: mb.y, Z: mb.z, OnGround: true}, dx, dz, g, mb.params)
	moved := nav.Distance(mb.at(), nav.Point{X: s.X, Z: s.Z})
	if moved > 0 {
		mb.x, mb.z, mb.changed = s.X, s.Z, true
		mb.y = a.groundAt(s.X, s.Z, mb.y, mb.params.Step)
		a.bodies.moved(mb)
	}
	return moved
}

// pathTo is the way from mb to to within leash, round the cell it last stalled at, ending on to itself; nil if there's none.
func (a *Arena) pathTo(mb *mob, to nav.Point, leash float64) []nav.Point {
	var avoid []nav.Point
	if a.tick < mb.avoidUntil {
		avoid = []nav.Point{mb.avoid}
	}
	path := a.finder.Path(a.m.Grid, mb.at(), to, mb.def.Radius, mb.home, leash, avoid...)
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
		p := nav.Point{X: mb.home.X + float64(r*sin), Z: mb.home.Z + float64(r*cos)}
		if a.m.Grid.Fits(p, mb.def.Radius) {
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
	mb.target, mb.provokedUntil, mb.near, mb.waiting, mb.claimFor = kernel.CharacterID{}, 0, false, false, kernel.CharacterID{}
	mb.path, mb.pathFor, mb.homing, mb.movedAt = nil, kernel.CharacterID{}, true, a.tick
}

// goHome walks a mob that gave up back into its circle, blind to players; caught on something too long, it stops and wanders. A standing boss is calm again once its way home ends.
func (a *Arena) goHome(mb *mob) {
	if nav.Distance(mb.at(), mb.home) <= mb.homeR || a.tick > mb.movedAt+a.ticks(stuckFor) {
		mb.homing, mb.path, mb.calm = false, nil, mb.spawn == noSpawn
		return
	}
	if len(mb.path) == 0 {
		to, ok := a.spotIn(mb)
		if !ok {
			to = mb.home
		}
		if mb.path = a.pathTo(mb, to, nav.Distance(mb.at(), mb.home)+mb.homeR); len(mb.path) == 0 {
			mb.path = []nav.Point{to} // no way the grid knows, as after a knock past a wall: straight, sliding along what's there
		}
	}
	a.walk(mb, mb.speed()*a.interval.Seconds())
	if mb.stalled {
		mb.pauseUntil = a.tick + a.ticks(repathEvery)
	}
}

// chase walks a mob to its spot beside p, looking for a new way in its turn as p moves or its walk stalls; it gives up when p is past its leash or it gets nowhere.
func (a *Arena) chase(mb *mob, p Body) {
	if mb.pathFor != p.ID {
		mb.path, mb.movedAt = nil, a.tick
	}
	if nav.Distance(p.at(), mb.home) > a.leash(mb) || a.tick > mb.movedAt+a.ticks(stuckFor) {
		a.giveUp(mb)
		a.goHome(mb)
		return
	}
	_, open := a.claimed(mb, p)
	due := mb.pathFor != p.ID || mb.stalled || a.tick >= mb.pathAt+a.ticks(repathEvery) || nav.Distance(mb.pathTo, p.at()) > repathMoved || !open
	if due && a.tick >= mb.pathAt+a.ticks(repathSoon) && a.searches > 0 {
		a.searches--
		mb.spot = a.claim(mb, p)
		mb.path = a.pathTo(mb, mb.spot, a.leash(mb))
		mb.pathFor, mb.pathAt, mb.pathTo, mb.stalled = p.ID, a.tick, p.at(), false
		mb.noWay = len(mb.path) == 0
	}
	mb.waiting = mb.pathFor == p.ID && nav.Distance(mb.at(), mb.spot) <= arrived && !a.inReach(mb, p)
	if mb.pathFor == p.ID && nav.Distance(mb.at(), mb.spot) <= arrived {
		mb.movedAt, mb.noWay = a.tick, false // there, where it waits rather than gives up
	}
	if mb.pathFor != p.ID || len(mb.path) == 0 {
		mb.face(p)
		return
	}
	a.walk(mb, mb.speed()*a.interval.Seconds())
	if len(mb.path) == 0 {
		mb.face(p)
	}
}

// ringRadius is how far from p's centre mb's ring stands: the contact ring, then, when its attack reaches short of the next, a ring at that reach's edge, then a mob width further out each.
func ringRadius(mb *mob, p Body, ring int) float64 {
	r := mb.def.Radius
	contact := p.Radius + r + contactGap
	edge := p.Radius + r + mb.reachGap() - contactGap
	if between := edge > contact && edge < contact+2*r; between && ring == 1 {
		return edge
	} else if between && ring > 1 {
		ring--
	}
	return contact + float64(float64(ring)*2*r)
}

// claimPoint is where mb's claim stands about p: on its ring at its angle, out of any portal; false past a mob width beyond the ring.
func (a *Arena) claimPoint(mb *mob, p Body, ring int, angle float64) (nav.Point, bool) {
	r := mb.def.Radius
	reach := ringRadius(mb, p, ring)
	sin, cos := math.Sincos(angle)
	d := a.pastPortals(p.at(), sin, cos, reach, r)
	return nav.Point{X: p.X + float64(d*sin), Z: p.Z + float64(d*cos)}, d < reach+2*r
}

// open reports whether mb may stand at at by p: inside its leash, out of portals, fitting, and clear of every other claim on p and every mob standing still without one.
func (a *Arena) open(mb *mob, p Body, at nav.Point) bool {
	a.hold(mb, p)
	return a.free(mb, at)
}

// hold gathers the places the other mobs keep mb's claims on p clear of: their own claims there, and where those standing still stand, unless on their claim; one pushed off its claim holds both.
func (a *Arena) hold(mb *mob, p Body) {
	a.held = a.held[:0]
	for _, o := range a.mobs {
		if o == mb {
			continue
		}
		claimed := false
		if o.claimFor == p.ID {
			if at, ok := a.claimPoint(o, p, o.claimRing, o.claimAngle); ok {
				a.held = append(a.held, place{at, o.def.Radius})
				claimed = nav.Distance(at, o.at()) <= crowdedBy
			}
		}
		if !claimed && (len(o.path) == 0 || o.attack >= 0 || o.pose != model.Standing) {
			a.held = append(a.held, place{o.at(), o.def.Radius})
		}
	}
}

// free reports whether mb may stand at at: inside its leash, out of portals, fitting, and clear of every place held.
func (a *Arena) free(mb *mob, at nav.Point) bool {
	r := mb.def.Radius
	if nav.Distance(at, mb.home) > a.leash(mb) || !a.offPortals(at, r) || !a.m.Grid.Fits(at, r) {
		return false
	}
	for _, h := range a.held {
		if nav.Distance(at, h.at) < r+h.r {
			return false
		}
	}
	return true
}

// claimed is mb's claimed spot by p, if it has one there still open.
func (a *Arena) claimed(mb *mob, p Body) (nav.Point, bool) {
	if mb.claimFor != p.ID {
		return nav.Point{}, false
	}
	at, ok := a.claimPoint(mb, p, mb.claimRing, mb.claimAngle)
	return at, ok && a.open(mb, p, at)
}

// claim is mb's spot on p's rings (3d-world.md section 7): the open claim it has unless a nearer ring has room, else the first ring outward with an open slot on the half facing it; p's point if there's none.
func (a *Arena) claim(mb *mob, p Body) nav.Point {
	kept, ok := a.claimed(mb, p)
	limit := rings
	if ok {
		limit = mb.claimRing
	}
	for ring := range limit {
		if at, angle, found := a.slot(mb, p, ring); found {
			mb.claimFor, mb.claimRing, mb.claimAngle = p.ID, ring, angle
			return at
		}
	}
	if ok {
		return kept
	}
	mb.claimFor = kernel.CharacterID{}
	return p.at()
}

// slot is the open spot on mb's ring about p nearest its own angle within a quarter turn, trying steps of half a width and the angles just clear of each place held.
func (a *Arena) slot(mb *mob, p Body, ring int) (nav.Point, float64, bool) {
	r, radius := mb.def.Radius, ringRadius(mb, p, ring)
	own, turn := math.Atan2(mb.x-p.X, mb.z-p.Z), r/radius
	a.angles = a.angles[:0]
	for k := 0; float64(float64(k)*turn) <= math.Pi/2; k++ {
		a.angles = append(a.angles, float64(k)*turn, -float64(k)*turn)
	}
	a.hold(mb, p)
	for _, h := range a.held {
		d, gap := nav.Distance(h.at, p.at()), r+h.r+touching
		if cos := (float64(radius*radius) + float64(d*d) - float64(gap*gap)) / float64(2*radius*d); d > 0 && math.Abs(cos) <= 1 {
			at := math.Atan2(h.at.X-p.X, h.at.Z-p.Z) - own
			a.angles = append(a.angles, math.Remainder(at+math.Acos(cos), 2*math.Pi), math.Remainder(at-math.Acos(cos), 2*math.Pi))
		}
	}
	slices.SortFunc(a.angles, func(x, y float64) int { return cmp.Or(cmp.Compare(math.Abs(x), math.Abs(y)), cmp.Compare(y, x)) }) // nearest first, the positive turn of a pair first
	for _, off := range a.angles {
		if math.Abs(off) > math.Pi/2 {
			break
		}
		if at, on := a.claimPoint(mb, p, ring, own+off); on && a.free(mb, at) {
			return at, own + off, true
		}
	}
	return nav.Point{}, 0, false
}

// pastPortals is the least distance from d on along (ux, uz) from p at which a mob of radius r is clear of every portal its way meets.
func (a *Arena) pastPortals(p nav.Point, ux, uz, d, r float64) float64 {
	for range len(a.portals) {
		moved := false
		for _, q := range a.portals {
			ox, oz := p.X-q.X, p.Z-q.Z
			b := float64(ux*ox) + float64(uz*oz)
			c := float64(ox*ox) + float64(oz*oz) - float64((q.Radius+r)*(q.Radius+r))
			if far := float64(d*d) + float64(2*d*b) + c; far >= 0 {
				continue // d is clear of q
			}
			d, moved = -b+math.Sqrt(max(0, float64(b*b)-c)), true
		}
		if !moved {
			break
		}
	}
	return d
}

// offPortals reports whether a mob of radius r at at keeps out of every portal.
func (a *Arena) offPortals(at nav.Point, r float64) bool {
	for _, p := range a.portals {
		if nav.Distance(at, nav.Point{X: p.X, Z: p.Z}) < p.Radius+r {
			return false
		}
	}
	return true
}

// way is how readily mb makes way for a mob it overlaps: one standing idle most, then one walking; one mid-attack or staggered not at all.
func (mb *mob) way() int {
	switch {
	case mb.attack >= 0 || mb.pose != model.Standing:
		return 0
	case len(mb.path) > 0:
		return 1
	}
	return 2
}

// spread drifts mobs that overlap apart, so a crowd that met on a spot ends side by side, and a pile chasing a player doesn't walk on deep in itself, where none may attack. The readier of two alone makes way; two idle ones each give half, and two walking ones half of what's deeper than crowdedBy, so neither is pushed off the end of its walk. Each mob's drifts add up to at most driftSpeed.
func (a *Arena) spread() {
	most := driftSpeed * a.interval.Seconds()
	a.pushes = slices.Grow(a.pushes[:0], len(a.mobs))[:len(a.mobs)]
	clear(a.pushes)
	for i, mb := range a.mobs {
		near := a.mobsNear(mb.x, mb.z, mb.x, mb.z, mb.def.Radius+a.bodies.reach) // past it, none overlaps mb
		for other, ok := near.next(); ok; other, ok = near.next() {
			j := other.slot
			if j <= i {
				continue
			}
			mbWay, otherWay := mb.way(), other.way()
			dx, dz := other.x-mb.x, other.z-mb.z
			d := nav.Distance(mb.at(), other.at())
			overlap := mb.def.Radius + other.def.Radius - d
			if mbWay == 1 && otherWay == 1 {
				overlap -= crowdedBy
			}
			if mbWay == 0 && otherWay == 0 || overlap <= 0 || other.y >= mb.y+mb.def.Height || mb.y >= other.y+other.def.Height {
				continue
			}
			if d == 0 {
				dx, dz, d = 1, 0, 1 // on one point: the later mob goes east
			}
			step := min(overlap/2, most)
			if mbWay != otherWay {
				step = min(overlap, most) // the readier one alone makes way
			}
			ux, uz := float64(dx/d*step), float64(dz/d*step)
			if mbWay >= otherWay {
				a.pushes[i].X, a.pushes[i].Z = a.pushes[i].X-ux, a.pushes[i].Z-uz
			}
			if otherWay >= mbWay {
				a.pushes[j].X, a.pushes[j].Z = a.pushes[j].X+ux, a.pushes[j].Z+uz
			}
		}
	}
	for i, mb := range a.mobs {
		if l := math.Hypot(a.pushes[i].X, a.pushes[i].Z); l > 0 {
			k := min(1, most/l)
			a.move(mb, float64(a.pushes[i].X*k), float64(a.pushes[i].Z*k), &a.blocking)
		}
	}
}
