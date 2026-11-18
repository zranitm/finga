package mobs

import (
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
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

// Landing is one of a mob's attacks landing this tick, an index into its type's attacks, on the players it reached, what it inflicts on those it lands on, and whether a block can't stop it.
type Landing struct {
	Mob         uint64
	Attack      int
	Hits        []Hit
	Inflicts    []model.Infliction
	Unblockable bool
	power       model.Attacker
}

// shot is a spit in flight: a ball of its attack's size, its centre at (x, y, z), flying along (dx, dz) with left metres to go.
type shot struct {
	mob     *mob
	attack  int
	x, y, z float64
	dx, dz  float64
	left    float64
}

const (
	sightAbove = 3.0         // metres over a mob's feet a player's may be and be seen
	sightBelow = 1.0         // and under
	sightKeep  = time.Second // how long a target out of sight stays the target
	targetHold = time.Second // how long a target holds before another who hits the mob takes over
	frontAngle = 90.0        // degrees: a bite's arc
	crowdedBy  = 0.25        // metres another mob may overlap one before it starts no attack, though it walks: two that met on a spot part before either attacks
	touchSlack = 1e-9        // metres apart a body still touches: the sim stops one at contact give or take float error
)

// turn is a mob's tick: a push, its attack, a stagger, the way home, or coming after its target or wandering. A status that blocks moving keeps it where it stands, one that blocks acting starts no attack, and both leave it still.
func (a *Arena) turn(mb *mob, players []Body) {
	a.settle(mb)
	if a.pushed(mb) {
		return
	}
	if mb.attack >= 0 {
		a.advance(mb, players)
		if mb.attack >= 0 {
			return
		}
	}
	if mb.pose == model.Flinching || mb.pose == model.Down {
		return
	}
	held, still := mb.held(), mb.statuses.Blocks(gamedata.BlocksAct)
	if held && still {
		return
	}
	if !mb.homing && a.strayed(mb) {
		a.giveUp(mb)
	}
	if mb.homing {
		if mb.pauseUntil <= a.tick && !held {
			a.goHome(mb)
		}
		return
	}
	if a.m.Kind == gamedata.Town {
		if !held {
			a.wander(mb)
		}
		return
	}
	target, ok := a.targetOf(mb, players)
	if !ok {
		mb.near, mb.waiting, mb.claimFor = false, false, kernel.CharacterID{}
		if !held {
			a.wander(mb)
		}
		return
	}
	reached := a.inReach(mb, target)
	if reached || a.shoots(mb, target) {
		mb.movedAt = a.tick // where it means to be
	}
	stuck := !reached && (mb.stalled || mb.noWay || mb.waiting)
	near := reached || stuck && slices.ContainsFunc(players, func(p Body) bool { return a.inReach(mb, p) }) // it can't get at its target: whoever it reaches
	if near && !mb.near {
		mb.engageAt = a.engageAt()
	}
	mb.near = near
	own := !a.crowded(mb) // a place of its own, which no mob standing still overlaps
	victim, k := target, -1
	if own && !still {
		k = a.readyFor(mb, target)
		if k < 0 && stuck {
			victim, k = a.readyOn(mb, players)
		}
	}
	switch {
	case k >= 0 && a.tick < mb.engageAt:
		mb.face(victim) // it squares up to them before attacking
	case k >= 0:
		mb.face(victim)
		a.begin(mb, k, victim, players)
	case own && a.shoots(mb, target):
		mb.path, mb.pathFor, mb.waiting, mb.claimFor = nil, kernel.CharacterID{}, false, kernel.CharacterID{}
		mb.face(target) // a ranged mob holds where its shot reaches
	case held:
		mb.face(target) // it turns to them where it stands
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

// targetOf is whom the mob comes after: its provoker once its target held a while, its target kept a second out of sight, or the nearest it sees.
func (a *Arena) targetOf(mb *mob, players []Body) (Body, bool) {
	k := slices.IndexFunc(players, func(p Body) bool { return p.ID == mb.target })
	held := !mb.target.IsZero() && k >= 0
	if held && a.sees(mb, players[k]) {
		mb.seenAt = a.tick
	}
	if p, ok := a.provokerOf(mb, players); ok && (!held || p.ID == mb.target || a.tick >= mb.targetAt+a.ticks(targetHold)) {
		a.take(mb, p.ID)
		if a.sees(mb, p) {
			mb.seenAt = a.tick
		}
		return p, true
	}
	if held && (mb.provokedUntil > a.tick || a.tick-mb.seenAt < a.ticks(sightKeep)) {
		return players[k], true
	}
	var best Body
	found := false
	for _, p := range players {
		if a.sees(mb, p) && (!found || nav.Distance(p.at(), mb.at()) < nav.Distance(best.at(), mb.at())) {
			best, found = p, true
		}
	}
	a.take(mb, best.ID)
	mb.seenAt = a.tick
	return best, found
}

// take makes id the mob's target, from this tick if it's new.
func (a *Arena) take(mb *mob, id kernel.CharacterID) {
	if id != mb.target {
		mb.target, mb.targetAt = id, a.tick
	}
}

// sees is whether p is within the mob's sight in x and z, their feet from sightBelow under its own to sightAbove over, and they haven't made it lose sight of them; solids hide nobody.
func (a *Arena) sees(mb *mob, p Body) bool {
	return nav.Distance(p.at(), mb.at()) <= a.data.MobRules().Sight && p.Y >= mb.y-sightBelow && p.Y <= mb.y+sightAbove && !mb.lost(p.ID)
}

// afterBlow is the tick a span d that a blow starts runs out: blows land after the tick's turns, so it counts from the next.
func (a *Arena) afterBlow(d time.Duration) uint64 { return a.tick + 1 + a.ticks(d) }

// provokerOf is the last player whose blow reached the mob, or who taunted it, while the provoke lasts, they're alive on the map, and it can see them.
func (a *Arena) provokerOf(mb *mob, players []Body) (Body, bool) {
	if mb.provokedUntil <= a.tick || mb.lost(mb.provoker) {
		return Body{}, false
	}
	k := slices.IndexFunc(players, func(p Body) bool { return p.ID == mb.provoker })
	if k < 0 {
		return Body{}, false
	}
	return players[k], true
}

// react staggers a mob without super armor that a blow from aim by player by landed on: a flinch and a push away, a skill's push its own, or at DownHits a knockdown.
func (a *Arena) react(mb *mob, from model.Aim, by kernel.CharacterID, push float64) {
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
	if mb.downCount >= r.DownHits && a.inflict(mb, a.knockdown(by)) {
		return
	}
	if push > 0 {
		a.shove(mb, from, push)
		return
	}
	if mb.phase == windingUp {
		mb.attack, mb.phase = -1, idle
	}
	mb.pose, mb.poseEnd = model.Flinching, a.afterBlow(r.Flinch)
	ux, uz := away(from.X, from.Z, mb.x, mb.z, float32(from.Yaw))
	a.move(mb, float64(ux*r.Push), float64(uz*r.Push), a.blockers)
}

// away is the unit vector from (fx, fz) to (tx, tz) in x and z; along yaw where they meet.
func away(fx, fz, tx, tz float64, yaw float32) (float64, float64) {
	d := nav.Distance(nav.Point{X: fx, Z: fz}, nav.Point{X: tx, Z: tz})
	if d == 0 {
		return model.HeadingOf(yaw)
	}
	return (tx - fx) / d, (tz - fz) / d
}

// begin starts attack k's windup at target, whom the mob faces, fixing a leap's landing or a rush's end so a player can step out; a rush ends touching the first mob in its lane, as they stand now, unless a player comes first.
func (a *Arena) begin(mb *mob, k int, target Body, players []Body) {
	at := mb.def.Attacks[k]
	mb.attack, mb.phase, mb.phaseEnd = k, windingUp, a.tick+a.ticks(at.Windup)
	mb.ready[k] = a.tick + a.ticks(at.Cooldown)
	mb.next = (k + 1) % len(mb.def.Attacks)
	mb.path, mb.pathFor, mb.movedAt = nil, kernel.CharacterID{}, a.tick // it's elsewhere after, and finds a new way
	mb.dest = mb.at()
	switch at.Kind {
	case gamedata.Leap: // it lands touching them, not on them
		mb.dest = a.ahead(mb, max(0, min(at.Distance, nav.Distance(mb.at(), target.at())-mb.def.Radius-target.Radius)))
	case gamedata.Rush:
		mb.dest = a.shortOfMobs(mb, a.ahead(mb, mb.distance(k)), players)
	}
	a.started = append(a.started, model.MobAttacked{
		Tick: a.tick, Mob: mb.id, Attack: k, Yaw: mb.yaw,
		ToX: mb.dest.X, ToY: a.groundAt(mb.dest.X, mb.dest.Z, mb.y, mb.params.Step), ToZ: mb.dest.Z,
	})
}

// ahead is where a move of dist along the mob's yaw ends: stopped by solids and portals as the sim stops a body, short of its leash.
func (a *Arena) ahead(mb *mob, dist float64) nav.Point {
	ux, uz := model.HeadingOf(mb.yaw)
	ox, oz := mb.x-mb.home.X, mb.z-mb.home.Z
	b, c := float64(ux*ox)+float64(uz*oz), float64(ox*ox)+float64(oz*oz)-float64(a.leash(mb)*a.leash(mb))
	dist = max(0, min(dist, -b+math.Sqrt(max(0, float64(b*b)-c)))) // the line's way out of the leash circle
	g := a.m.Geometry
	g.Blockers = a.portals
	s := sim.Push(sim.State{X: mb.x, Y: mb.y, Z: mb.z, OnGround: true}, float64(ux*dist), float64(uz*dist), g, mb.params)
	return nav.Point{X: s.X, Z: s.Z}
}

// shortOfMobs is to, or where mb's body first touches another mob on its way there, unless it meets a player first.
func (a *Arena) shortOfMobs(mb *mob, to nav.Point, players []Body) nav.Point {
	d := nav.Distance(mb.at(), to)
	if d == 0 {
		return to
	}
	ux, uz := (to.X-mb.x)/d, (to.Z-mb.z)/d
	if t, deep, ok := a.mobAhead(mb, ux, uz, d); ok {
		if _, met := first(players, mb.at(), ux, uz, min(deep, d), mb.def.Radius, mb.y, mb.y+mb.def.Height); !met {
			return nav.Point{X: mb.x + float64(ux*t), Z: mb.z + float64(uz*t)}
		}
	}
	return to
}

// mobAhead is how far mb goes along (ux, uz) before a mob cuts it, if one does within dist, and before it would be crowdedBy deeper into any mob than now.
func (a *Arena) mobAhead(mb *mob, ux, uz, dist float64) (cut, deep float64, found bool) {
	cut, deep = dist, math.Inf(1)
	for _, o := range a.mobs {
		if o == mb || o.y >= mb.y+mb.def.Height || mb.y >= o.y+o.def.Height {
			continue
		}
		ox, oz := o.x-mb.x, o.z-mb.z
		along := float64(ox*ux) + float64(oz*uz)
		r := mb.def.Radius + o.def.Radius
		apart := math.Hypot(ox, oz)
		across := float64(apart*apart) - float64(along*along) // squared
		if graze := r - crowdedBy; along <= 0 || across >= float64(graze*graze) {
			continue // behind it, or off its lane but for an edge
		}
		if t := max(0, along-math.Sqrt(float64(r*r)-across)); t < cut {
			cut, found = t, true
		}
		if near := min(apart, r) - crowdedBy; near > 0 && float64(near*near) > across { // as near as it comes crowdedBy deeper than now
			deep = min(deep, along-math.Sqrt(float64(near*near)-across))
		}
	}
	return cut, deep, found
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

// travel moves a leap or a rush a tick's share of the way: a rush ends on the first player it meets, or touching a mob that stepped into its lane; a leap passes over players. A mob held where it is ends a rush there, and lands a leap there in its time.
func (a *Arena) travel(mb *mob, players []Body) {
	at := mb.def.Attacks[mb.attack]
	if mb.held() {
		if at.Kind == gamedata.Rush || a.tick >= mb.phaseEnd {
			if at.Kind == gamedata.Leap {
				a.landArea(mb, players)
			}
			a.recover(mb)
		}
		return
	}
	left := uint64(1)
	if mb.phaseEnd > a.tick {
		left = mb.phaseEnd - a.tick + 1
	}
	if d := nav.Distance(mb.at(), mb.dest); d > 0 || at.Kind == gamedata.Rush {
		step := d / float64(left)
		ux, uz := model.HeadingOf(mb.yaw) // a rush cut to nothing still meets a player touching it
		if d > 0 {
			ux, uz = (mb.dest.X-mb.x)/d, (mb.dest.Z-mb.z)/d
		}
		if at.Kind == gamedata.Rush {
			went, deep, stopped := a.mobAhead(mb, ux, uz, step)
			reach := went
			if stopped {
				reach = min(deep, step) // a player met before it goes crowdedBy deeper into a mob is met first
			}
			p, ok := first(players, mb.at(), ux, uz, reach, mb.def.Radius, mb.y, mb.y+mb.def.Height)
			if ok {
				went = reach // on to the player, who stops it
			}
			a.move(mb, float64(ux*went), float64(uz*went), a.blockers)
			if ok {
				a.land(mb, []Hit{{Player: p.ID, DX: ux, DZ: uz}})
			}
			if ok || stopped {
				a.recover(mb)
				return
			}
		} else {
			a.move(mb, float64(ux*step), float64(uz*step), a.portals) // a leap passes over players
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
		a.landings = append(a.landings, a.landing(mb, mb.attack, hits))
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
		end, went := a.ballStep(at, s.x, s.y, s.z, s.dx, s.dz, move)
		if p, ok := first(players, nav.Point{X: s.x, Z: s.z}, s.dx, s.dz, went, r, s.y-r, s.y+r); ok {
			a.landings = append(a.landings, a.landing(s.mob, s.attack, []Hit{{Player: p.ID, DX: s.dx, DZ: s.dz}}))
			return true
		}
		s.x, s.z, s.left = end.X, end.Z, s.left-move
		return went < move-1e-9 || s.left <= 0 // short of its move: a solid or a rise stopped it
	})
}

// ballStep moves at's ball, its centre at (x, y, z), move along (dx, dz): to its end, and how far it went, short of move where a solid or a rise stopped it.
func (a *Arena) ballStep(at gamedata.MobAttack, x, y, z, dx, dz, move float64) (nav.Point, float64) {
	r := at.Size / 2
	ball := sim.Params{Radius: r, Height: at.Size} // a body that steps onto nothing, so any solid or rise over its bottom stops it
	s := sim.Push(sim.State{X: x, Y: y - r, Z: z}, float64(dx*move), float64(dz*move), a.m.Geometry, ball)
	end := nav.Point{X: s.X, Z: s.Z}
	return end, nav.Distance(nav.Point{X: x, Z: z}, end)
}

// clear is whether shot k's ball, thrown at p from where mb stands, meets them before a solid or a rise stops it, flying as fly moves it.
func (a *Arena) clear(mb *mob, k int, p Body) bool {
	at := mb.def.Attacks[k]
	d := nav.Distance(mb.at(), p.at())
	if d == 0 {
		return true
	}
	ux, uz := (p.X-mb.x)/d, (p.Z-mb.z)/d
	r, y := at.Size/2, mb.y+mb.def.Height/2
	them := [1]Body{p}
	for from, left := mb.at(), at.Range; left > 0; {
		move := min(at.Speed*a.interval.Seconds(), left)
		to, went := a.ballStep(at, from.X, y, from.Z, ux, uz, move)
		if _, ok := first(them[:], from, ux, uz, went, r, y-r, y+r); ok {
			return true
		}
		if went < move-1e-9 {
			return false
		}
		from, left = to, left-move
	}
	return false
}

// first is the player nearest along the way that a circle of radius r, from lo to hi high, meets swept dist from from along (ux, uz).
func first(players []Body, from nav.Point, ux, uz, dist, r, lo, hi float64) (Body, bool) {
	var best Body
	bestAlong, found := 0.0, false
	for _, p := range players {
		if p.Y > hi || p.Y+p.Height < lo {
			continue
		}
		px, pz := p.X-from.X, p.Z-from.Z
		along := float64(px*ux) + float64(pz*uz)
		t := max(0, min(along, dist))
		if nav.Distance(nav.Point{X: px, Z: pz}, nav.Point{X: float64(t * ux), Z: float64(t * uz)}) > r+p.Radius+touchSlack {
			continue
		}
		if !found || along < bestAlong {
			best, bestAlong, found = p, along, true
		}
	}
	return best, found
}

// crowded reports whether mb has no place of its own: a mob standing still overlaps its body at all, or any mob, walking or not, by more than crowdedBy.
func (a *Arena) crowded(mb *mob) bool {
	for _, o := range a.mobs {
		if o == mb || o.y >= mb.y+mb.def.Height || mb.y >= o.y+o.def.Height {
			continue
		}
		overlap := mb.def.Radius + o.def.Radius - nav.Distance(mb.at(), o.at())
		still := len(o.path) == 0 || o.attack >= 0 || o.pose != model.Standing
		if overlap > crowdedBy || still && overlap > 1e-6 {
			return true
		}
	}
	return false
}

// inReach is whether any of the mob's attacks, turned to p, would reach them.
func (a *Arena) inReach(mb *mob, p Body) bool {
	for k := range mb.def.Attacks {
		if a.reaches(mb, k, p) {
			return true
		}
	}
	return false
}

// readyOn is the nearest player a ready attack reaches, and the attack; -1 for none.
func (a *Arena) readyOn(mb *mob, players []Body) (Body, int) {
	var best Body
	k := -1
	for _, p := range players {
		if j := a.readyFor(mb, p); j >= 0 && (k < 0 || nav.Distance(p.at(), mb.at()) < nav.Distance(best.at(), mb.at())) {
			best, k = p, j
		}
	}
	return best, k
}

// readyFor is the attack to start at p, turned to them: a ready one that reaches them, the one in turn first, and no leap or rush while held where it stands; -1 for none.
func (a *Arena) readyFor(mb *mob, p Body) int {
	held := mb.held()
	for j := range mb.def.Attacks {
		k := (mb.next + j) % len(mb.def.Attacks)
		if kind := mb.def.Attacks[k].Kind; held && (kind == gamedata.Leap || kind == gamedata.Rush) {
			continue
		}
		if mb.ready[k] <= a.tick && a.reaches(mb, k, p) {
			return k
		}
	}
	return -1
}

// shoots is whether one of the mob's shots, turned to p, would reach them.
func (a *Arena) shoots(mb *mob, p Body) bool {
	for k, at := range mb.def.Attacks {
		if at.Kind == gamedata.Shot && a.reaches(mb, k, p) {
			return true
		}
	}
	return false
}

// reaches is whether attack k, the mob turned to p, could hit them: its own body, then what the kind adds as its windup would fix it; a shot's ball must get to them past solids and rises.
func (a *Arena) reaches(mb *mob, k int, p Body) bool {
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
		return gap+mb.def.Radius <= at.Range+at.Size/2 && p.Y <= mid+at.Size/2 && p.Y+p.Height >= mid-at.Size/2 && a.clear(mb, k, p)
	case gamedata.Around:
		return gap <= at.Spread && mb.spans(p, at.Spread)
	}
	return false
}

// lands is whether attack k, as the mob stands and faces, hits p: its body, a bite's arc, or a slam's or a leap's circle.
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

// reachGap is how far past the mob's edge its longest-reaching attack but a shot hits.
func (mb *mob) reachGap() float64 {
	far := 0.0
	for k, at := range mb.def.Attacks {
		switch at.Kind {
		case gamedata.Front:
			far = max(far, at.Reach)
		case gamedata.Leap:
			far = max(far, at.Distance+at.Spread)
		case gamedata.Rush:
			far = max(far, mb.distance(k))
		case gamedata.Around:
			far = max(far, at.Spread)
		}
	}
	return far
}

// distance is how far attack k carries the mob: a rush at its own pace, a leap as far as the attack says.
func (mb *mob) distance(k int) float64 {
	if at := mb.def.Attacks[k]; at.Kind == gamedata.Rush {
		return at.Distance * mb.pace
	}
	return mb.def.Attacks[k].Distance
}
