package mobs

import (
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/nav"
	"finga/internal/sim"
	"finga/modules/world/internal/model"
)

// phase is where a mob is in its attack.
type phase int

const (
	idle phase = iota
	windingUp
	travelling // a leap or a rush
	recovering
)

// Body is a living player as the mobs meet them.
type Body struct {
	ID kernel.CharacterID
	model.Body
}

func (b Body) at() nav.Point { return nav.Point{X: b.X, Z: b.Z} }

// Hit is a player an attack reached, and the way it knocks them: a unit vector in x and z.
type Hit struct {
	Player kernel.CharacterID
	DX, DZ float64
}

// Landing is one of a mob's attacks landing this tick, an index into its type's attacks, on the players it reached.
type Landing struct {
	Mob    uint64
	Attack int
	Hits   []Hit
	power  model.Attacker
}

// shot is a spit in flight: a ball of its attack's size, its centre at (x, y, z), flying along (dx, dz) with left metres to go.
type shot struct {
	mob     *mob
	attack  int
	x, y, z float64
	dx, dz  float64
	left    float64
}

// A mob sees a player whose feet are from sightBelow under its own to sightAbove over, in metres, and keeps them as its target
// until they've been out of its sight sightKeep.
const (
	sightAbove = 3.0
	sightBelow = 1.0
	sightKeep  = time.Second
	frontAngle = 90.0 // degrees: a bite's arc
)

// turn is a mob's tick: its attack, a stagger, the way home, or coming after its target or wandering.
func (a *Arena) turn(mb *mob, players []Body) {
	a.settle(mb)
	if mb.attack >= 0 {
		a.advance(mb, players)
		if mb.attack >= 0 {
			return
		}
	}
	if mb.pose == model.Flinching || mb.pose == model.Down {
		return
	}
	if !mb.homing && a.strayed(mb) {
		a.giveUp(mb)
	}
	if mb.homing {
		if mb.pauseUntil <= a.tick {
			a.goHome(mb)
		}
		return
	}
	if a.m.Kind == gamedata.Town {
		a.wander(mb)
		return
	}
	target, ok := a.targetOf(mb, players)
	if !ok {
		mb.near = false
		a.wander(mb)
		return
	}
	reached := mb.inReach(target)
	stuck := !reached && (mb.stalled || mb.noWay)
	near := reached || stuck && slices.ContainsFunc(players, mb.inReach) // it can't get at its target: whoever it reaches
	if near && !mb.near {
		mb.engageAt = a.engageAt()
	}
	mb.near = near
	victim, k := target, mb.readyFor(a.tick, target)
	if k < 0 && stuck {
		victim, k = mb.readyOn(a.tick, players)
	}
	switch {
	case k >= 0 && a.tick < mb.engageAt:
		mb.face(victim) // it squares up to them before attacking
	case k >= 0:
		mb.face(victim)
		a.begin(mb, k, victim)
	case mb.shoots(target):
		mb.face(target) // a ranged mob holds where its shot reaches
	default:
		a.chase(mb, target)
	}
}

// settle moves a mob's pose on as its time runs out, a knockdown into rising, whatever the mob is doing.
func (a *Arena) settle(mb *mob) {
	if mb.pose == model.Standing || a.tick < mb.poseEnd {
		return
	}
	if mb.pose == model.Down {
		mb.pose, mb.poseEnd, mb.downCount = model.Rising, a.tick+a.ticks(a.data.MobRules().React.ArmorAfter), 0
	} else {
		mb.pose = model.Standing
	}
	mb.changed = true
}

// engageAt is the tick a mob with someone come into its reach, after nobody was, may start a windup: a wait rolled in the rules' engage.
func (a *Arena) engageAt() uint64 {
	r := a.data.MobRules()
	if d := a.rolled(r.EngageMin, r.EngageMax); d > 0 {
		return a.tick + a.ticks(d)
	}
	return a.tick
}

// rolled is a time rolled from lo to hi.
func (a *Arena) rolled(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(a.roll.Float64()*float64(hi-lo))
}

// targetOf is whom the mob comes after: its provoker, the target it keeps a second out of sight, or the nearest it sees.
func (a *Arena) targetOf(mb *mob, players []Body) (Body, bool) {
	if p, ok := a.provokerOf(mb, players); ok {
		mb.target = p.ID
		if a.sees(mb, p) {
			mb.seenAt = a.tick
		}
		return p, true
	}
	if k := slices.IndexFunc(players, func(p Body) bool { return p.ID == mb.target }); !mb.target.IsZero() && k >= 0 {
		if a.sees(mb, players[k]) {
			mb.seenAt = a.tick
		}
		if a.tick-mb.seenAt < a.ticks(sightKeep) {
			return players[k], true
		}
	}
	var best Body
	found := false
	for _, p := range players {
		if a.sees(mb, p) && (!found || nav.Distance(p.at(), mb.at()) < nav.Distance(best.at(), mb.at())) {
			best, found = p, true
		}
	}
	mb.target, mb.seenAt = best.ID, a.tick
	return best, found
}

// sees is whether p is in the mob's sight: within its rules' sight in x and z, their feet from sightBelow under its own to
// sightAbove over. Solids don't hide them.
func (a *Arena) sees(mb *mob, p Body) bool {
	return nav.Distance(p.at(), mb.at()) <= a.data.MobRules().Sight && p.Y >= mb.y-sightBelow && p.Y <= mb.y+sightAbove
}

// afterBlow is the tick a span d that a blow starts runs out: blows land after the tick's turns, so it counts from the next.
func (a *Arena) afterBlow(d time.Duration) uint64 { return a.tick + 1 + a.ticks(d) }

// provokerOf is the last player whose blow reached the mob, while the provoke lasts and they're alive on the map.
func (a *Arena) provokerOf(mb *mob, players []Body) (Body, bool) {
	if mb.provokedUntil <= a.tick {
		return Body{}, false
	}
	k := slices.IndexFunc(players, func(p Body) bool { return p.ID == mb.provoker })
	if k < 0 {
		return Body{}, false
	}
	return players[k], true
}

// react staggers a mob without super armor that a blow from aim landed on: a flinch and a push away from the attacker, or a
// knockdown at DownHits.
func (a *Arena) react(mb *mob, from model.Aim) {
	if mb.def.SuperArmor || mb.pose == model.Down || mb.pose == model.Rising {
		return
	}
	r := a.data.MobRules().React
	if a.tick-mb.lastHit >= a.ticks(r.DownWindow) {
		mb.downCount = 0
	}
	mb.lastHit = a.tick
	mb.downCount++
	mb.changed = true
	if mb.downCount >= r.DownHits {
		mb.attack, mb.phase = -1, idle // a spit already thrown flies on
		mb.pose, mb.poseEnd = model.Down, a.afterBlow(r.Down)
		return
	}
	if mb.phase == windingUp {
		mb.attack, mb.phase = -1, idle
	}
	mb.pose, mb.poseEnd = model.Flinching, a.afterBlow(r.Flinch)
	ux, uz := away(from.X, from.Z, mb.x, mb.z, float32(from.Yaw))
	a.move(mb, ux*r.Push, uz*r.Push, a.blockers)
}

// away is the unit vector from (fx, fz) to (tx, tz) in x and z; along yaw where they meet.
func away(fx, fz, tx, tz float64, yaw float32) (float64, float64) {
	d := nav.Distance(nav.Point{X: fx, Z: fz}, nav.Point{X: tx, Z: tz})
	if d == 0 {
		return model.HeadingOf(yaw)
	}
	return (tx - fx) / d, (tz - fz) / d
}

// begin starts attack k's windup at target, whom the mob faces; a leap's landing and a rush's end are fixed now, so a player can
// step out of them.
func (a *Arena) begin(mb *mob, k int, target Body) {
	at := mb.def.Attacks[k]
	mb.attack, mb.phase, mb.phaseEnd = k, windingUp, a.tick+a.ticks(at.Windup)
	mb.ready[k] = a.tick + a.ticks(at.Cooldown)
	mb.next = (k + 1) % len(mb.def.Attacks)
	mb.path, mb.pathFor = nil, kernel.CharacterID{} // it's elsewhere after, and finds a new way
	mb.dest = mb.at()
	switch at.Kind {
	case gamedata.Leap:
		mb.dest = a.ahead(mb, min(at.Distance, nav.Distance(mb.at(), target.at())))
	case gamedata.Rush:
		mb.dest = a.ahead(mb, mb.distance(k))
	}
	a.started = append(a.started, model.MobAttacked{
		Tick: a.tick, Mob: mb.id, Attack: k, Yaw: mb.yaw,
		ToX: mb.dest.X, ToY: a.groundAt(mb.dest.X, mb.dest.Z, mb.y, mb.params.Step), ToZ: mb.dest.Z,
	})
}

// ahead is where a move of dist along the mob's yaw ends: stopped by solids as the sim stops a body, and short of its leash.
func (a *Arena) ahead(mb *mob, dist float64) nav.Point {
	ux, uz := model.HeadingOf(mb.yaw)
	ox, oz := mb.x-mb.home.X, mb.z-mb.home.Z
	b, c := ux*ox+uz*oz, ox*ox+oz*oz-a.leash(mb)*a.leash(mb)
	dist = max(0, min(dist, -b+math.Sqrt(max(0, b*b-c)))) // the line's way out of the leash circle
	s := sim.Push(sim.State{X: mb.x, Y: mb.y, Z: mb.z, OnGround: true}, ux*dist, uz*dist, a.m.Geometry, mb.params)
	return nav.Point{X: s.X, Z: s.Z}
}

// advance moves a mob's attack on a tick: the windup ends in the act, a leap or a rush travels, and the recover ends it.
func (a *Arena) advance(mb *mob, players []Body) {
	at := mb.def.Attacks[mb.attack]
	switch mb.phase {
	case windingUp:
		if a.tick < mb.phaseEnd {
			return
		}
		switch at.Kind {
		case gamedata.Leap, gamedata.Rush:
			mb.phase, mb.phaseEnd = travelling, a.tick+a.ticks(at.Travel)
			return
		case gamedata.Shot:
			a.throw(mb, players)
		default:
			a.landArea(mb, players)
		}
		a.recover(mb)
	case travelling:
		a.travel(mb, players)
	case recovering:
		if a.tick >= mb.phaseEnd {
			mb.attack, mb.phase = -1, idle
		}
	}
}

func (a *Arena) recover(mb *mob) {
	mb.phase, mb.phaseEnd = recovering, a.tick+a.ticks(mb.def.Attacks[mb.attack].Recover)
}

// travel moves a leap or a rush a tick's share of the way to its end: a rush ends on the first player its body meets, and stops at
// them; a leap passes over players and lands on everyone in its circle.
func (a *Arena) travel(mb *mob, players []Body) {
	at := mb.def.Attacks[mb.attack]
	left := uint64(1)
	if mb.phaseEnd > a.tick {
		left = mb.phaseEnd - a.tick + 1
	}
	if d := nav.Distance(mb.at(), mb.dest); d > 0 {
		step := d / float64(left)
		ux, uz := (mb.dest.X-mb.x)/d, (mb.dest.Z-mb.z)/d
		if at.Kind == gamedata.Rush {
			p, ok := first(players, mb.at(), ux, uz, step, mb.def.Radius, mb.y, mb.y+mb.def.Height)
			a.move(mb, ux*step, uz*step, a.blockers)
			if ok {
				a.land(mb, []Hit{{Player: p.ID, DX: ux, DZ: uz}})
				a.recover(mb)
				return
			}
		} else {
			a.move(mb, ux*step, uz*step, nil)
		}
	}
	if a.tick < mb.phaseEnd {
		return
	}
	if at.Kind == gamedata.Leap {
		a.landArea(mb, players)
	}
	a.recover(mb)
}

// landArea lands the mob's attack on every player in its area as it stands: a bite's arc ahead, or a slam's or a leap's circle.
func (a *Arena) landArea(mb *mob, players []Body) {
	var hits []Hit
	for _, p := range players {
		if mb.lands(mb.attack, p) {
			ux, uz := away(mb.x, mb.z, p.X, p.Z, mb.yaw)
			hits = append(hits, Hit{Player: p.ID, DX: ux, DZ: uz})
		}
	}
	a.land(mb, hits)
}

func (a *Arena) land(mb *mob, hits []Hit) {
	if len(hits) > 0 {
		a.landings = append(a.landings, Landing{Mob: mb.id, Attack: mb.attack, Hits: hits, power: model.MobAttacker(mb.def, mb.def.Attacks[mb.attack].Power)})
	}
}

// throw lets a spit go from the mob's centre at mid-height along its yaw; a player its body touches takes it at once.
func (a *Arena) throw(mb *mob, players []Body) {
	ux, uz := model.HeadingOf(mb.yaw)
	if p, ok := first(players, mb.at(), ux, uz, 0, mb.def.Radius, mb.y, mb.y+mb.def.Height); ok {
		a.land(mb, []Hit{{Player: p.ID, DX: ux, DZ: uz}})
		return
	}
	at := mb.def.Attacks[mb.attack]
	a.shots = append(a.shots, &shot{mob: mb, attack: mb.attack, x: mb.x, y: mb.y + mb.def.Height/2, z: mb.z, dx: ux, dz: uz, left: at.Range})
}

// fly moves each spit a tick: it lands on the first player its path meets, and ends there, at a solid or a rise, or at its range.
func (a *Arena) fly(players []Body) {
	dt := a.interval.Seconds()
	a.shots = slices.DeleteFunc(a.shots, func(s *shot) bool {
		at := s.mob.def.Attacks[s.attack]
		r := at.Size / 2
		move := min(at.Speed*dt, s.left)
		ball := sim.Params{Radius: r, Height: at.Size} // a body that steps onto nothing, so any solid or rise over its bottom stops it
		end := sim.Push(sim.State{X: s.x, Y: s.y - r, Z: s.z}, s.dx*move, s.dz*move, a.m.Geometry, ball)
		went := nav.Distance(nav.Point{X: s.x, Z: s.z}, nav.Point{X: end.X, Z: end.Z})
		if p, ok := first(players, nav.Point{X: s.x, Z: s.z}, s.dx, s.dz, went, r, s.y-r, s.y+r); ok {
			a.landings = append(a.landings, Landing{Mob: s.mob.id, Attack: s.attack, Hits: []Hit{{Player: p.ID, DX: s.dx, DZ: s.dz}}, power: model.MobAttacker(s.mob.def, at.Power)})
			return true
		}
		s.x, s.z, s.left = end.X, end.Z, s.left-move
		return went < move-1e-9 || s.left <= 0 // short of its move: a solid or a rise stopped it
	})
}

// first is the player met first by a circle of radius r swept from from along (ux, uz) for dist, its height from lo to hi: the
// one nearest along the way.
func first(players []Body, from nav.Point, ux, uz, dist, r, lo, hi float64) (Body, bool) {
	var best Body
	bestAlong, found := 0.0, false
	for _, p := range players {
		if p.Y > hi || p.Y+p.Height < lo {
			continue
		}
		px, pz := p.X-from.X, p.Z-from.Z
		along := px*ux + pz*uz
		t := max(0, min(along, dist))
		if nav.Distance(nav.Point{X: px, Z: pz}, nav.Point{X: t * ux, Z: t * uz}) > r+p.Radius {
			continue
		}
		if !found || along < bestAlong {
			best, bestAlong, found = p, along, true
		}
	}
	return best, found
}

// inReach is whether any of the mob's attacks, turned to p, would reach them.
func (mb *mob) inReach(p Body) bool {
	for k := range mb.def.Attacks {
		if mb.reaches(k, p) {
			return true
		}
	}
	return false
}

// readyOn is the nearest player a ready attack reaches, and the attack; -1 for none.
func (mb *mob) readyOn(tick uint64, players []Body) (Body, int) {
	var best Body
	k := -1
	for _, p := range players {
		if j := mb.readyFor(tick, p); j >= 0 && (k < 0 || nav.Distance(p.at(), mb.at()) < nav.Distance(best.at(), mb.at())) {
			best, k = p, j
		}
	}
	return best, k
}

// readyFor is the attack to start at p, turned to them: a ready one that reaches them, the one in turn first; -1 for none.
func (mb *mob) readyFor(tick uint64, p Body) int {
	for j := range mb.def.Attacks {
		if k := (mb.next + j) % len(mb.def.Attacks); mb.ready[k] <= tick && mb.reaches(k, p) {
			return k
		}
	}
	return -1
}

// shoots is whether one of the mob's shots, turned to p, would reach them.
func (mb *mob) shoots(p Body) bool {
	for k, at := range mb.def.Attacks {
		if at.Kind == gamedata.Shot && mb.reaches(k, p) {
			return true
		}
	}
	return false
}

// reaches is whether attack k, the mob turned to p, could hit them: its own body, then what the kind adds, as far as its windup
// would fix it now.
func (mb *mob) reaches(k int, p Body) bool {
	if mb.touches(p, 0) {
		return true
	}
	at := mb.def.Attacks[k]
	gap := nav.Distance(mb.at(), p.at()) - p.Radius - mb.def.Radius // from the mob's edge to theirs
	switch at.Kind {
	case gamedata.Front:
		return gap <= at.Reach && mb.spans(p, 0)
	case gamedata.Leap:
		return gap <= at.Distance+at.Spread && mb.spans(p, at.Spread)
	case gamedata.Rush:
		return gap <= mb.distance(k) && mb.spans(p, 0)
	case gamedata.Shot:
		mid := mb.y + mb.def.Height/2
		return gap+mb.def.Radius <= at.Range+at.Size/2 && p.Y <= mid+at.Size/2 && p.Y+p.Height >= mid-at.Size/2
	case gamedata.Around:
		return gap <= at.Spread && mb.spans(p, at.Spread)
	}
	return false
}

// lands is whether attack k, as the mob stands and faces, hits p: a bite's arc of its radius plus Reach, or a slam's or a leap's
// circle of its radius plus Spread; its own body hits too.
func (mb *mob) lands(k int, p Body) bool {
	at := mb.def.Attacks[k]
	switch at.Kind {
	case gamedata.Front:
		aim := model.Aim{X: mb.x, Y: mb.y, Z: mb.z, Yaw: float64(mb.yaw)}
		arc := gamedata.Shape{Kind: gamedata.ArcShape, Radius: mb.def.Radius + at.Reach, Angle: frontAngle, Height: mb.def.Height}
		return mb.touches(p, 0) || len(model.Reached(aim, arc, []model.Body{p.Body}, 1)) > 0
	case gamedata.Leap, gamedata.Around:
		return mb.touches(p, at.Spread)
	}
	return mb.touches(p, 0)
}

// touches is whether p meets the mob's body grown by grow on every side.
func (mb *mob) touches(p Body, grow float64) bool {
	return nav.Distance(mb.at(), p.at()) <= mb.def.Radius+grow+p.Radius && mb.spans(p, grow)
}

// spans is whether p's height meets the mob's grown by grow above and below.
func (mb *mob) spans(p Body, grow float64) bool {
	return p.Y <= mb.y+mb.def.Height+grow && p.Y+p.Height >= mb.y-grow
}

// distance is how far attack k carries the mob: a rush at its own pace, a leap as far as the attack says.
func (mb *mob) distance(k int) float64 {
	if at := mb.def.Attacks[k]; at.Kind == gamedata.Rush {
		return at.Distance * mb.pace
	}
	return mb.def.Attacks[k].Distance
}
