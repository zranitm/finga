// Package mobs runs a map's mobs: their spawns, patrols and attacks, and how a player's strike resolves against them.
package mobs

import (
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/model"
)

const noSpawn = -1 // a boss's spawn: it never respawns

// History is how many ticks of mob boxes the arena keeps, the oldest an input's view may name.
const History = 8

// framed is the living mobs' boxes as one tick's frame left them.
type framed struct {
	tick  uint64
	boxes []model.Rect
}

// mob patrols its area of its ground, pausing at an edge and turning, until it sees a player; then it follows them along that ground and attacks.
type mob struct {
	id            uint64
	spawn         int // index into the map's spawn areas; noSpawn for a boss
	def           gamedata.Mob
	hp            int
	x, y          float64 // feet centre
	facing        int
	patrolX       bounds // where it patrols: its area of its ground
	chaseX        bounds // where it goes after a player: its ground from end to end, up to a wall or a block
	pauseUntil    uint64
	changed       bool
	attack        int // the attack under way, an index into def.Attacks; -1 for none
	phase         phase
	phaseEnd      uint64
	dest, step    float64  // a leap's or a rush's end x, and its move a tick
	ready         []uint64 // by attack, the tick it may start again
	next          int      // the attack in turn
	provoker      kernel.CharacterID
	provokedUntil uint64
	pace          float64            // its speed's factor, rolled at its spawn
	target        kernel.CharacterID // whom it comes after; zero for none
	seenAt        uint64             // the tick it last saw its target
	engageAt      uint64             // the tick it may start its first windup on a target it picked up, having had none
	pose          model.MobPose
	poseEnd       uint64
	downCount     int    // blows landed, each within the down window of the last
	lastHit       uint64 // the tick of the last
}

// bounds is an x range a mob's feet centre keeps to.
type bounds struct{ lo, hi float64 }

func (b bounds) clamp(x float64) float64 { return min(max(x, b.lo), b.hi) }

// around is the bounds from lo to hi, or x alone where a mob there has no room to walk.
func around(x, lo, hi float64) bounds {
	if hi <= lo {
		return bounds{x, x}
	}
	return bounds{lo, hi}
}

type spawnState struct {
	alive   int
	returns []uint64 // ticks at which a replacement spawns
}

func (mb *mob) box() model.Rect { return mb.boxAt(mb.x) }

func (mb *mob) boxAt(x float64) model.Rect {
	return model.Rect{X: x - mb.def.Width/2, Y: mb.y - mb.def.Height, W: mb.def.Width, H: mb.def.Height}
}

// sweep is all the mob's box covers on its way from x from to x to.
func (mb *mob) sweep(from, to float64) model.Rect {
	a, b := mb.boxAt(from), mb.boxAt(to)
	left := min(a.X, b.X)
	return model.Rect{X: left, Y: a.Y, W: max(a.X, b.X) + a.W - left, H: a.H}
}

func (mb *mob) view() model.MobView {
	return model.MobView{ID: mb.id, Type: mb.def.ID, X: mb.x, Y: mb.y, Facing: mb.facing, HP: mb.hp, MaxHP: mb.def.HP, Pose: mb.pose}
}

// speed is the mob's px/s at its own pace.
func (mb *mob) speed() float64 { return mb.def.Speed * mb.pace }

// Arena is one map's mobs; only the map's tick goroutine touches it.
type Arena struct {
	m        *gamedata.Map
	data     *gamedata.Data
	roll     model.Roll
	interval time.Duration // the map's tick

	tick    uint64
	mobs    []*mob // spawn order; removed on death
	spawns  []spawnState
	nextMob uint64
	removed []uint64 // this tick's
	hit     gamedata.HitCurve
	history [History]framed // by tick mod History

	started  []model.MobAttacked // this tick's
	landings []Landing           // this tick's
	shots    []*shot
	boxes    []model.Rect // this tick's living players, whom a pushed mob stops at
}

// Deps are the map the arena runs; all are required.
type Deps struct {
	Map  *gamedata.Map
	Data *gamedata.Data
	Roll model.Roll // the map's random source, used only on its tick goroutine
}

// Tuning is the map's tick.
type Tuning struct {
	TickInterval time.Duration
}

// NewArena spawns every area's mobs on the map, so it is full before its first tick.
func NewArena(d Deps, t Tuning) *Arena {
	rules := d.Data.StatRules()
	a := &Arena{m: d.Map, data: d.Data, roll: d.Roll, interval: t.TickInterval, spawns: make([]spawnState, len(d.Map.Spawns)), hit: rules.Hit}
	for s, area := range d.Map.Spawns {
		for range area.Max {
			a.spawn(s)
		}
	}
	return a
}

// Step starts tick n: respawns, spits in flight, then each mob's turn among the living players.
func (a *Arena) Step(n uint64, players []Body) {
	a.tick = n
	a.removed, a.started, a.landings = a.removed[:0], a.started[:0], a.landings[:0]
	for s := range a.m.Spawns {
		a.respawn(s)
	}
	a.fly(players)
	a.boxes = a.boxes[:0]
	for _, p := range players {
		a.boxes = append(a.boxes, p.Box)
	}
	for _, mb := range a.mobs {
		from, facing := mb.x, mb.facing
		a.turn(mb, players, a.boxes)
		mb.changed = mb.changed || mb.x != from || mb.facing != facing
	}
}

// Started are the attacks this tick's windups began.
func (a *Arena) Started() []model.MobAttacked { return a.started }

// Landings are the attacks landing this tick, those of a mob killed since gone.
func (a *Arena) Landings() []Landing { return a.landings }

// Roll resolves landing l on player id, defending with d.
func (a *Arena) Roll(l Landing, id kernel.CharacterID, d model.Defender) model.Damage {
	h := model.Resolve(l.power, d, a.hit, a.roll)
	return model.Damage{Tick: a.tick, Source: model.EntityRef{Mob: l.Mob}, Target: model.EntityRef{Player: id}, Amount: h.Damage, Crit: h.Crit, Miss: h.Miss}
}

func (a *Arena) ticks(d time.Duration) uint64 { return model.TicksIn(d, a.interval) }

// stopAt is where a move from from toward to ends: its x overlap with each player it meets or would pass doesn't grow.
func (mb *mob) stopAt(from, to float64, players []model.Rect) float64 {
	was := mb.boxAt(from)
	for _, p := range players {
		if mb.sweep(from, to).Overlaps(p) {
			to = sim.Held(was, from, to, p)
		}
	}
	return to
}

func (a *Arena) respawn(s int) {
	area := a.m.Spawns[s]
	st := &a.spawns[s]
	for len(st.returns) > 0 && st.returns[0] <= a.tick && st.alive < area.Max {
		st.returns = st.returns[1:]
		a.spawn(s)
	}
}

func (a *Arena) spawn(s int) {
	area := a.m.Spawns[s]
	def, ok := a.data.Mob(area.Mob)
	if !ok {
		return
	}
	a.nextMob++
	half := def.Width / 2
	x := area.Area.X + half + a.roll.Float64()*math.Max(0, area.Area.W-def.Width)
	y := area.Area.Y + area.Area.H
	a.place(&mob{id: a.nextMob, spawn: s, def: def, hp: def.HP, x: x, y: y}, area.Area)
	a.spawns[s].alive++
}

// SpawnBoss raises a field boss at its area's bottom centre, outside the spawn areas, so it never respawns; it returns the boss's mob ID.
func (a *Arena) SpawnBoss(def gamedata.Mob, area model.Rect) uint64 {
	a.nextMob++
	a.place(&mob{id: a.nextMob, spawn: noSpawn, def: def, hp: def.HP, x: area.X + area.W/2, y: area.Y + area.H}, area)
	return a.nextMob
}

// place puts mb on the map facing a random way, at its own pace, to patrol the ground under it within area.
func (a *Arena) place(mb *mob, area model.Rect) {
	mb.facing, mb.changed, mb.attack, mb.ready = 1, true, -1, make([]uint64, len(mb.def.Attacks))
	if a.roll.Float64() < 0.5 {
		mb.facing = -1
	}
	mb.pace = 1 + a.data.MobRules().Patrol.SpeedJitter*(2*a.roll.Float64()-1)
	mb.chaseX = a.chaseBounds(mb, area)
	mb.patrolX = a.patrolBounds(mb, area)
	a.mobs = append(a.mobs, mb)
}

// patrolBounds is where mb patrols: the ground under it within area, else the area, inside its chase bounds.
func (a *Arena) patrolBounds(mb *mob, area model.Rect) bounds {
	half := mb.def.Width / 2
	lo, hi := area.X+half, area.X+area.W-half
	if r, ok := a.m.GroundAt(mb.x, mb.y); ok {
		lo, hi = max(r.X, area.X)+half, min(r.X+r.W, area.X+area.W)-half
	}
	return around(mb.x, max(lo, mb.chaseX.lo), min(hi, mb.chaseX.hi))
}

// chaseBounds is where mb may go after a player: the ground under it, cut at the nearest solid its box would walk into on each side; else area.
func (a *Arena) chaseBounds(mb *mob, area model.Rect) bounds {
	half := mb.def.Width / 2
	ground, ok := a.groundAt(mb.x, mb.y)
	if !ok {
		return around(mb.x, area.X+half, area.X+area.W-half)
	}
	lo, hi := ground.X, ground.X+ground.W
	band := model.Rect{X: lo, Y: mb.y - mb.def.Height, W: ground.W, H: mb.def.Height}
	for _, s := range a.m.Geometry.Solids {
		switch {
		case !s.Overlaps(band):
		case s.X+s.W <= mb.x:
			lo = max(lo, s.X+s.W)
		case s.X >= mb.x:
			hi = min(hi, s.X)
		}
	}
	return around(mb.x, lo+half, hi-half)
}

// groundAt is the solid or platform under feet at (x, y), joined with each level with it that it runs onto, as a platform onto a pillar's top.
func (a *Arena) groundAt(x, y float64) (model.Rect, bool) {
	r, ok := a.m.GroundAt(x, y)
	pieces := slices.Concat(a.m.Geometry.Solids, a.m.Geometry.Platforms)
	for joined := ok; joined; {
		joined = false
		for _, p := range pieces {
			if math.Abs(p.Y-r.Y) < 1 && p.X <= r.X+r.W && p.X+p.W >= r.X && (p.X < r.X || p.X+p.W > r.X+r.W) {
				lo, hi := min(r.X, p.X), max(r.X+r.W, p.X+p.W)
				r.X, r.W, joined = lo, hi-lo, true
			}
		}
	}
	return r, ok
}

// Strike is a player's hit, a basic attack or a skill, from where they stand the way they face.
type Strike struct {
	By     kernel.CharacterID
	Shape  gamedata.Shape
	Most   int // the most mobs it hits
	Power  model.Attacker
	X, Y   float64 // the attacker's feet centre
	Facing int
	Height float64 // the attacker's; a shot flies at half of it, an area spreads from it
}

// Blow is what a strike did to one mob it reached: its damage, the HP it took, which overkill leaves out, and the mob if it died.
type Blow struct {
	Damage model.Damage
	Dealt  int
	Killed *Kill
}

// Kill is a mob a strike killed, as its drops and its EXP need it.
type Kill struct {
	Mob  gamedata.Mob
	X, Y float64
}

// Strike resolves a hit against each mob its shape reaches, nearest first; a killed mob leaves the map and waits to respawn.
func (a *Arena) Strike(s Strike) []Blow {
	targets := make([]model.Target, 0, len(a.mobs))
	for _, mb := range a.mobs {
		targets = append(targets, model.Target{Box: mb.box()})
	}
	reached := model.Reached(s.X, s.Y, s.Facing, s.Height, s.Shape, targets, s.Most)
	hit := make([]*mob, 0, len(reached))
	for _, idx := range reached {
		hit = append(hit, a.mobs[idx]) // before a kill removes any of them
	}
	blows := make([]Blow, 0, len(hit))
	for _, mb := range hit {
		mb.provoker, mb.provokedUntil = s.By, a.afterBlow(a.data.MobRules().Provoke) // a MISS provokes too
		h := model.Resolve(s.Power, model.MobDefender(mb.def), a.hit, a.roll)
		b := Blow{Damage: model.Damage{Tick: a.tick, Source: model.EntityRef{Player: s.By}, Target: model.EntityRef{Mob: mb.id}, Amount: h.Damage, Crit: h.Crit, Miss: h.Miss}}
		if !h.Miss {
			b.Dealt = min(h.Damage, mb.hp)
			mb.hp -= h.Damage
			mb.changed = true
			if mb.hp <= 0 {
				a.remove(mb)
				b.Killed = &Kill{Mob: mb.def, X: mb.x, Y: mb.y}
			} else {
				a.react(mb, s.X, s.Facing)
			}
		}
		blows = append(blows, b)
	}
	return blows
}

// Remove takes a mob off the map without a kill, as a boss nobody beat leaves; false if it is not there.
func (a *Arena) Remove(id uint64) bool {
	k := slices.IndexFunc(a.mobs, func(mb *mob) bool { return mb.id == id })
	if k < 0 {
		return false
	}
	a.remove(a.mobs[k])
	return true
}

// remove takes mb off the map with its attacks: what lands this tick, and its spits in flight.
func (a *Arena) remove(mb *mob) {
	a.mobs = slices.DeleteFunc(a.mobs, func(m *mob) bool { return m == mb })
	a.landings = slices.DeleteFunc(a.landings, func(l Landing) bool { return l.Mob == mb.id })
	a.shots = slices.DeleteFunc(a.shots, func(s *shot) bool { return s.mob == mb })
	a.removed = append(a.removed, mb.id)
	if mb.spawn == noSpawn {
		return
	}
	st := &a.spawns[mb.spawn]
	st.alive--
	st.returns = append(st.returns, a.tick+model.TicksIn(a.m.Spawns[mb.spawn].Respawn, a.interval))
}

// Views are every mob, as a Snapshot shows them.
func (a *Arena) Views() []model.MobView {
	views := make([]model.MobView, 0, len(a.mobs))
	for _, mb := range a.mobs {
		views = append(views, mb.view())
	}
	return views
}

// Changes are the tick's moved, spawned or hurt mobs and those removed, each once; it keeps the frame's boxes for BoxesAt.
func (a *Arena) Changes() (changed []model.MobView, removed []uint64) {
	kept := &a.history[a.tick%History]
	kept.tick, kept.boxes = a.tick, kept.boxes[:0]
	for _, mb := range a.mobs {
		kept.boxes = append(kept.boxes, mb.box())
		if mb.changed {
			changed = append(changed, mb.view())
			mb.changed = false
		}
	}
	return changed, slices.Clone(a.removed)
}

// BoxesAt are the living mobs' boxes as tick t's frame left them; none once t is older than History ticks, or not yet sent.
func (a *Arena) BoxesAt(t uint64) []model.Rect {
	if kept := a.history[t%History]; kept.tick == t {
		return kept.boxes
	}
	return nil
}
