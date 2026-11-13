package mobs

import (
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/model"
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
	ID  kernel.CharacterID
	Box model.Rect
}

func (b Body) centre() float64 { return b.Box.X + b.Box.W/2 }

// Hit is a player an attack reached, and the way it knocks them: 1 right, -1 left.
type Hit struct {
	Player kernel.CharacterID
	Dir    int
}

// Landing is one of a mob's attacks landing this tick, an index into its type's attacks, on the players it reached.
type Landing struct {
	Mob    uint64
	Attack int
	Hits   []Hit
	power  model.Attacker
}

// shot is a spit in flight: a square of its attack's size at height y, its back edge at x, with left px to go.
type shot struct {
	mob    *mob
	attack int
	x, y   float64
	dir    int
	left   float64
}

// A mob sees a player whose feet are from its own up to this many px higher, and keeps them as its target until they've been out of its sight this long.
const (
	sightAbove = 96.0
	sightKeep  = time.Second
)

// turn is a mob's tick: its attack, a stagger, or coming after its target or patrolling.
func (a *Arena) turn(mb *mob, players []Body, boxes []model.Rect) {
	a.settle(mb)
	if mb.attack >= 0 {
		a.advance(mb, players, boxes)
		if mb.attack >= 0 {
			return
		}
	}
	if mb.pose == model.Flinching || mb.pose == model.Down {
		return
	}
	if a.m.Kind == gamedata.Town {
		a.patrol(mb, boxes)
		return
	}
	target, ok := a.targetOf(mb, players)
	if !ok {
		mb.near = false
		a.patrol(mb, boxes)
		return
	}
	reached := mb.inReach(target)
	stuck := !reached && a.stuck(mb, target, boxes)
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
		mb.facing = mb.toward(victim) // it squares up to them before attacking
	case k >= 0:
		mb.facing = mb.toward(victim)
		a.begin(mb, k, victim)
	case mb.shoots(target):
		mb.facing = mb.toward(target) // a ranged mob holds where its shot reaches
	default:
		a.chase(mb, target, boxes)
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

// patrol walks a mob a chase took out of its patrol bounds back in, with no pause; inside, to their edge, where it waits a rolled while and turns.
func (a *Arena) patrol(mb *mob, boxes []model.Rect) {
	if home := mb.patrolX.clamp(mb.x); home != mb.x {
		mb.facing = mb.towardX(home)
		mb.x = a.stepToward(mb, home, boxes)
		return
	}
	if mb.pauseUntil > a.tick || mb.patrolX.hi <= mb.patrolX.lo {
		return
	}
	mb.x = mb.stopAt(mb.x, mb.x+float64(mb.facing)*mb.speed()*a.interval.Seconds(), boxes)
	switch {
	case mb.x >= mb.patrolX.hi:
		mb.x, mb.facing, mb.pauseUntil = mb.patrolX.hi, -1, a.tick+a.ticks(a.pause())
	case mb.x <= mb.patrolX.lo:
		mb.x, mb.facing, mb.pauseUntil = mb.patrolX.lo, 1, a.tick+a.ticks(a.pause())
	}
}

// pause is an edge pause, rolled in the patrol's range.
func (a *Arena) pause() time.Duration {
	p := a.data.MobRules().Patrol
	return a.rolled(p.PauseMin, p.PauseMax)
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

// chase walks a mob toward its target along its ground, with no pause, stopping at its chase bounds.
func (a *Arena) chase(mb *mob, p Body, boxes []model.Rect) {
	mb.facing = mb.toward(p)
	mb.x = a.stepToward(mb, p.centre(), boxes)
}

// stepToward is where a tick's walk toward x leaves the mob: never past x, within its chase bounds, held by players in its way.
func (a *Arena) stepToward(mb *mob, x float64, boxes []model.Rect) float64 {
	step := min(mb.speed()*a.interval.Seconds(), math.Abs(x-mb.x))
	return mb.stopAt(mb.x, mb.clampX(mb.x+float64(mb.towardX(x))*step), boxes)
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
		if a.sees(mb, p) && (!found || math.Abs(p.centre()-mb.x) < math.Abs(best.centre()-mb.x)) {
			best, found = p, true
		}
	}
	mb.target, mb.seenAt = best.ID, a.tick
	return best, found
}

// stuck is whether a step toward p would leave the mob where it is: at its chase bounds, held by a player in its way, or already under p.
func (a *Arena) stuck(mb *mob, p Body, boxes []model.Rect) bool {
	return a.stepToward(mb, p.centre(), boxes) == mb.x
}

// sees is whether p is in the mob's sight: within its rules' sight either way, their feet from its own up to sightAbove px higher. Solids don't hide them.
func (a *Arena) sees(mb *mob, p Body) bool {
	feet := p.Box.Y + p.Box.H
	return math.Abs(p.centre()-mb.x) <= a.data.MobRules().Sight && feet <= mb.y && feet >= mb.y-sightAbove
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

// react staggers a mob without super armor that a blow landed on: a flinch and push, or a knockdown at DownHits.
func (a *Arena) react(mb *mob, x float64, facing int) {
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
	dir := facing
	switch {
	case mb.x > x:
		dir = 1
	case mb.x < x:
		dir = -1
	}
	mb.x = mb.stopAt(mb.x, mb.clampX(mb.x+float64(dir)*r.Push), a.boxes)
}

// begin starts attack k's windup at target, whom the mob faces; a leap's landing and a rush's end are set now, so a player can step out of them.
func (a *Arena) begin(mb *mob, k int, target Body) {
	at := mb.def.Attacks[k]
	mb.attack, mb.phase, mb.phaseEnd = k, windingUp, a.tick+a.ticks(at.Windup)
	mb.ready[k] = a.tick + a.ticks(at.Cooldown)
	mb.next = (k + 1) % len(mb.def.Attacks)
	to := mb.x
	switch at.Kind {
	case gamedata.Leap:
		mb.dest = mb.reachX.clamp(mb.x + float64(mb.facing)*min(at.Distance, math.Abs(target.centre()-mb.x)))
		to = mb.dest
	case gamedata.Rush:
		mb.dest = mb.reachX.clamp(mb.x + float64(mb.facing)*mb.distance(k))
		to = mb.dest
	}
	a.started = append(a.started, model.MobAttacked{Tick: a.tick, Mob: mb.id, Attack: k, Facing: mb.facing, To: to})
}

// advance moves a mob's attack on a tick: the windup ends in the act, a leap or a rush travels, and the recover ends it.
func (a *Arena) advance(mb *mob, players []Body, boxes []model.Rect) {
	at := mb.def.Attacks[mb.attack]
	switch mb.phase {
	case windingUp:
		if a.tick < mb.phaseEnd {
			return
		}
		switch at.Kind {
		case gamedata.Leap, gamedata.Rush:
			travel := a.ticks(at.Travel)
			mb.phase, mb.phaseEnd, mb.step, mb.lead = travelling, a.tick+travel, (mb.dest-mb.x)/float64(travel), mb.x
			return
		case gamedata.Shot:
			a.throw(mb, players)
		default:
			a.landArea(mb, players)
		}
		a.recover(mb)
	case travelling:
		a.travel(mb, players, boxes)
	case recovering:
		if a.tick >= mb.phaseEnd {
			mb.attack, mb.phase = -1, idle
		}
	}
}

func (a *Arena) recover(mb *mob) {
	mb.phase, mb.phaseEnd = recovering, a.tick+a.ticks(mb.def.Attacks[mb.attack].Recover)
}

// travel moves a leap or a rush a step, its body following inside its chase bounds: a rush ends on the first player it sweeps over; a leap lands on everyone in its area.
func (a *Arena) travel(mb *mob, players []Body, boxes []model.Rect) {
	at := mb.def.Attacks[mb.attack]
	from, to := mb.lead, mb.lead+mb.step
	if a.tick >= mb.phaseEnd {
		to = mb.dest
	}
	mb.lead = to
	if at.Kind == gamedata.Rush {
		met, ok := first(players, func(p Body) bool { return p.Box.Overlaps(mb.sweep(from, to)) }, mb.facing, from)
		mb.x = mb.stopAt(mb.x, mb.clampX(to), boxes)
		if ok {
			a.land(mb, []Hit{{Player: met.ID, Dir: mb.toward(met)}})
			a.recover(mb)
			return
		}
	} else {
		mb.x = mb.clampX(to) // a leap passes over players
	}
	if a.tick < mb.phaseEnd {
		return
	}
	if at.Kind == gamedata.Leap {
		a.landArea(mb, players)
	}
	a.recover(mb)
}

// landArea lands the mob's attack on every player in its area as it stands.
func (a *Arena) landArea(mb *mob, players []Body) {
	area := mb.area(mb.attack)
	var hits []Hit
	for _, p := range players {
		if p.Box.Overlaps(area) {
			hits = append(hits, Hit{Player: p.ID, Dir: mb.toward(p)})
		}
	}
	a.land(mb, hits)
}

func (a *Arena) land(mb *mob, hits []Hit) {
	if len(hits) > 0 {
		a.landings = append(a.landings, Landing{Mob: mb.id, Attack: mb.attack, Hits: hits, power: model.MobAttacker(mb.def, mb.def.Attacks[mb.attack].Power)})
	}
}

// throw lets a spit go from the mob's front at mid-height; a player overlapping the mob as it's thrown takes it at once.
func (a *Arena) throw(mb *mob, players []Body) {
	own := mb.box()
	if p, ok := first(players, func(p Body) bool { return p.Box.Overlaps(own) }, mb.facing, mb.x); ok {
		a.land(mb, []Hit{{Player: p.ID, Dir: mb.facing}})
		return
	}
	at := mb.def.Attacks[mb.attack]
	front := own.X + own.W
	if mb.facing < 0 {
		front = own.X
	}
	a.shots = append(a.shots, &shot{mob: mb, attack: mb.attack, x: front, y: own.Y + own.H/2, dir: mb.facing, left: at.Range - at.Size}) // its front goes range px past the mob's
}

// fly moves each spit a tick: it lands on the first player its path meets, and ends there, at a solid, or at its range.
func (a *Arena) fly(players []Body) {
	dt := a.interval.Seconds()
	a.shots = slices.DeleteFunc(a.shots, func(s *shot) bool {
		at := s.mob.def.Attacks[s.attack]
		move := min(at.Speed*dt, s.left)
		path := s.path(move, at.Size)
		walled := false
		for _, solid := range a.m.Geometry.Solids {
			if !solid.Overlaps(path) {
				continue
			}
			walled = true
			if s.dir > 0 {
				path.W = max(0, solid.X-path.X)
			} else {
				path.W, path.X = max(0, path.X+path.W-(solid.X+solid.W)), solid.X+solid.W
			}
		}
		if p, ok := first(players, func(p Body) bool { return p.Box.Overlaps(path) }, s.dir, s.x); ok {
			a.landings = append(a.landings, Landing{Mob: s.mob.id, Attack: s.attack, Hits: []Hit{{Player: p.ID, Dir: s.dir}}, power: model.MobAttacker(s.mob.def, at.Power)})
			return true
		}
		s.x += float64(s.dir) * move
		s.left -= move
		return walled || s.left <= 0
	})
}

// path is the rect a spit's square sweeps moving move px.
func (s *shot) path(move, size float64) model.Rect {
	r := model.Rect{X: s.x, Y: s.y - size/2, W: size + move, H: size}
	if s.dir < 0 {
		r.X = s.x - size - move
	}
	return r
}

// first is the player meeting reached first going dir from x: the one behind x before those ahead, then the nearest.
func first(players []Body, reached func(Body) bool, dir int, x float64) (Body, bool) {
	var best Body
	found := false
	for _, p := range players {
		if !reached(p) {
			continue
		}
		if !found || float64(dir)*(p.centre()-x) < float64(dir)*(best.centre()-x) {
			best, found = p, true
		}
	}
	return best, found
}

// inReach is whether any of the mob's attacks, turned to p, would reach them.
func (mb *mob) inReach(p Body) bool {
	for k := range mb.def.Attacks {
		if mb.reaches(k, mb.toward(p), p.Box) {
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
		if j := mb.readyFor(tick, p); j >= 0 && (k < 0 || math.Abs(p.centre()-mb.x) < math.Abs(best.centre()-mb.x)) {
			best, k = p, j
		}
	}
	return best, k
}

// readyFor is the attack to start at p, turned to them: a ready one that reaches them, the one in turn first; -1 for none.
func (mb *mob) readyFor(tick uint64, p Body) int {
	for j := range mb.def.Attacks {
		if k := (mb.next + j) % len(mb.def.Attacks); mb.ready[k] <= tick && mb.reaches(k, mb.toward(p), p.Box) {
			return k
		}
	}
	return -1
}

// shoots is whether one of the mob's shots, turned to p, would reach them.
func (mb *mob) shoots(p Body) bool {
	for k, at := range mb.def.Attacks {
		if at.Kind == gamedata.Shot && mb.reaches(k, mb.toward(p), p.Box) {
			return true
		}
	}
	return false
}

// reaches is whether attack k, the mob facing that way, could hit box: its own box, then what the kind adds.
func (mb *mob) reaches(k, facing int, box model.Rect) bool {
	own := mb.box()
	if box.Overlaps(own) {
		return true
	}
	at := mb.def.Attacks[k]
	far := mb.reachX.clamp(mb.x + float64(facing)*mb.distance(k))
	switch at.Kind {
	case gamedata.Front:
		return box.Overlaps(ahead(own, facing, at.Reach))
	case gamedata.Leap:
		return box.Overlaps(grown(mb.span(far), at.Spread, 0))
	case gamedata.Rush:
		return box.Overlaps(mb.span(far))
	case gamedata.Shot:
		lane := ahead(own, facing, at.Range)
		lane.Y, lane.H = own.Y+own.H/2-at.Size/2, at.Size
		return box.Overlaps(lane)
	case gamedata.Around:
		return box.Overlaps(grown(own, at.Spread, at.Spread))
	}
	return false
}

// distance is how far attack k carries the mob: a rush at its own pace, a leap as far as the attack says.
func (mb *mob) distance(k int) float64 {
	if at := mb.def.Attacks[k]; at.Kind == gamedata.Rush {
		return at.Distance * mb.pace
	}
	return mb.def.Attacks[k].Distance
}

// area is where attack k lands, the mob's box with it: a bite's reach ahead and a slam's on every side as it stands; a leap's to each side where it came down.
func (mb *mob) area(k int) model.Rect {
	own := mb.box()
	at := mb.def.Attacks[k]
	switch at.Kind {
	case gamedata.Front:
		r := ahead(own, mb.facing, at.Reach)
		return model.Rect{X: min(own.X, r.X), Y: own.Y, W: own.W + at.Reach, H: own.H}
	case gamedata.Leap:
		return grown(mb.boxAt(mb.lead), at.Spread, 0)
	case gamedata.Around:
		return grown(own, at.Spread, at.Spread)
	}
	return own
}

// span is the mob's box swept from where it stands to x.
func (mb *mob) span(x float64) model.Rect {
	own := mb.box()
	return model.Rect{X: min(mb.x, x) - mb.def.Width/2, Y: own.Y, W: math.Abs(x-mb.x) + mb.def.Width, H: own.H}
}

// clampX is x held to the mob's chase bounds.
func (mb *mob) clampX(x float64) float64 { return mb.chaseX.clamp(x) }

// toward is the way to face p, and the way a hit knocks them.
func (mb *mob) toward(p Body) int { return mb.towardX(p.centre()) }

// towardX is the way to face x; under it, the way the mob faces.
func (mb *mob) towardX(x float64) int {
	switch {
	case x > mb.x:
		return 1
	case x < mb.x:
		return -1
	}
	return mb.facing
}

// ahead is the strip reach px wide before box's front, at its height.
func ahead(box model.Rect, facing int, reach float64) model.Rect {
	if facing > 0 {
		return model.Rect{X: box.X + box.W, Y: box.Y, W: reach, H: box.H}
	}
	return model.Rect{X: box.X - reach, Y: box.Y, W: reach, H: box.H}
}

// grown is r grown by dx on each side and dy above and below.
func grown(r model.Rect, dx, dy float64) model.Rect {
	return model.Rect{X: r.X - dx, Y: r.Y - dy, W: r.W + 2*dx, H: r.H + 2*dy}
}
