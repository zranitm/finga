// Package mobs runs a map's mobs, their spawns, wandering, chases and attacks, and players' strikes on them (3d-world.md section 7).
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

const noSpawn = -1 // a boss's spawn: it never respawns

// History is how many ticks of mob bodies the arena keeps, the oldest an input's view may name.
const History = 8

// framed is the living mobs as one tick's frame left them, as the sim's blockers.
type framed struct {
	tick     uint64
	blockers []sim.Blocker
}

// mob wanders its spawn circle until it sees a player, walks to its spot beside them and attacks, and walks home past its leash.
type mob struct {
	id      uint64
	spawn   int // index into the map's spawns; noSpawn for a boss
	def     gamedata.Mob
	params  sim.Params // the sim's, with its own radius and height: how it walks into solids and players
	hp      int
	x, y, z float64 // feet centre
	yaw     float32
	home    nav.Point // its spawn circle's centre
	homeR   float64   // and radius
	changed bool

	path       []nav.Point        // the points it walks through, the next first
	pathFor    kernel.CharacterID // whom its path leads to; zero for a wander or the way home
	pathAt     uint64             // the tick it found a path to them
	pathTo     nav.Point          // where they stood then
	spot       nav.Point          // its spot beside them
	stalled    bool               // its last walk went nowhere
	noWay      bool               // its last search for a way to its target found none
	avoid      nav.Point          // the point it last stalled walking to, which its searches route round
	avoidUntil uint64
	movedAt    uint64             // the last tick it got anywhere, or had no need to
	homing     bool               // led past its leash: walking home, blind to players until it's back in its circle
	waiting    bool               // at its spot by its target on its last chase, its target out of its reach
	claimFor   kernel.CharacterID // whose contact rings it has a spot on, zero for none
	claimRing  int
	claimAngle float64 // radians from north toward east about them

	pauseUntil    uint64
	attack        int // the attack under way, an index into def.Attacks; -1 for none
	phase         phase
	phaseEnd      uint64
	dest          nav.Point // a leap's or a rush's end, fixed at its windup
	ready         []uint64  // by attack, the tick it may start again
	next          int       // the attack in turn
	provoker      kernel.CharacterID
	provokedUntil uint64
	pace          float64            // its speed's factor, rolled at its spawn
	target        kernel.CharacterID // whom it comes after; zero for none
	targetAt      uint64             // the tick it took them
	seenAt        uint64             // the tick it last saw its target
	engageAt      uint64             // the tick it may start a windup on someone come into its reach, after nobody was
	near          bool               // someone was in its reach on its last turn
	pose          model.MobPose
	poseEnd       uint64
	downCount     int    // blows landed, each within the down window of the last
	lastHit       uint64 // the tick of the last
}

type spawnState struct {
	alive   int
	returns []uint64 // ticks at which a replacement spawns
}

func (mb *mob) at() nav.Point { return nav.Point{X: mb.x, Z: mb.z} }

func (mb *mob) body() model.Body {
	return model.Body{X: mb.x, Y: mb.y, Z: mb.z, Radius: mb.def.Radius, Height: mb.def.Height}
}

func (mb *mob) blocker() sim.Blocker {
	return sim.Blocker{X: mb.x, Z: mb.z, Radius: mb.def.Radius, Bottom: mb.y, Top: mb.y + mb.def.Height}
}

func (mb *mob) view() model.MobView {
	return model.MobView{ID: mb.id, Type: mb.def.ID, X: mb.x, Y: mb.y, Z: mb.z, Yaw: mb.yaw, HP: mb.hp, MaxHP: mb.def.HP, Pose: mb.pose}
}

// speed is the mob's m/s at its own pace.
func (mb *mob) speed() float64 { return mb.def.Speed * mb.pace }

// turnTo faces the mob yaw.
func (mb *mob) turnTo(yaw float32) {
	if yaw != mb.yaw {
		mb.yaw, mb.changed = yaw, true
	}
}

// face turns the mob to p; on p's spot, it keeps its yaw.
func (mb *mob) face(p Body) {
	if dx, dz := p.X-mb.x, p.Z-mb.z; dx != 0 || dz != 0 {
		mb.turnTo(model.YawOf(dx, dz))
	}
}

// place is a body's footprint on the ground.
type place struct {
	at nav.Point
	r  float64
}

// Arena is one map's mobs; only the map's tick goroutine touches it.
type Arena struct {
	m        *gamedata.Map
	data     *gamedata.Data
	roll     model.Roll
	interval time.Duration // the map's tick

	tick    uint64
	mobs    []*mob // spawn order, so id order; removed on death
	spawns  []spawnState
	nextMob uint64
	removed []uint64 // this tick's
	hit     gamedata.HitCurve
	history [History]framed // by tick mod History

	started  []model.MobAttacked // this tick's
	landings []Landing           // this tick's
	shots    []*shot
	portals  []sim.Blocker // the map's portals, which a mob never steps into, so a player can always leave by one
	blockers []sim.Blocker // the portals and this tick's living players, whom a mob's body stops at

	walkGrid *nav.Grid
	finder   nav.Finder
	searches int       // this tick's path searches left
	held     []place   // the places a claim keeps clear of, reused
	angles   []float64 // the turns off its own angle a claim tries, reused
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

// NewArena spawns every spawn's mobs on the map, so it is full before its first tick.
func NewArena(d Deps, t Tuning) *Arena {
	rules := d.Data.StatRules()
	a := &Arena{m: d.Map, data: d.Data, roll: d.Roll, interval: t.TickInterval, spawns: make([]spawnState, len(d.Map.Spawns)), hit: rules.Hit}
	for _, p := range d.Map.Portals {
		a.portals = append(a.portals, sim.Blocker{X: p.At.X, Z: p.At.Z, Radius: p.Radius, Bottom: math.Inf(-1), Top: math.Inf(1)})
	}
	for s, spawn := range d.Map.Spawns {
		for range spawn.Max {
			a.spawn(s)
		}
	}
	return a
}

// Step starts tick n: respawns, spits in flight, each mob's turn among the living players, from one mob further on each tick so none waits behind the others for its path searches, then idle mobs that overlap drift apart.
func (a *Arena) Step(n uint64, players []Body) {
	a.start(n, players)
	for k := range a.mobs {
		a.turn(a.mobs[(k+int(n%uint64(len(a.mobs))))%len(a.mobs)], players)
	}
	a.spread()
}

// Rest starts tick n on a map nobody is on: respawns, spits in flight, acts under way and walks home go on, but no mob looks for anyone or wanders, so an empty map costs next to nothing.
func (a *Arena) Rest(n uint64) {
	a.start(n, nil)
	for _, mb := range a.mobs {
		mb.near, mb.waiting, mb.claimFor = false, false, kernel.CharacterID{}
		a.settle(mb)
		if mb.attack >= 0 {
			a.advance(mb, nil)
		} else if mb.homing && mb.pauseUntil <= a.tick {
			a.goHome(mb)
		}
	}
}

func (a *Arena) start(n uint64, players []Body) {
	a.tick, a.searches = n, searches
	a.removed, a.started, a.landings = a.removed[:0], a.started[:0], a.landings[:0]
	a.blockers = append(a.blockers[:0], a.portals...)
	for _, p := range players {
		a.blockers = append(a.blockers, sim.Blocker{X: p.X, Z: p.Z, Radius: p.Radius, Bottom: p.Y, Top: p.Y + p.Height})
	}
	for s := range a.m.Spawns {
		a.respawn(s)
	}
	a.fly(players)
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

func (a *Arena) respawn(s int) {
	spawn := a.m.Spawns[s]
	st := &a.spawns[s]
	for len(st.returns) > 0 && st.returns[0] <= a.tick && st.alive < spawn.Max {
		st.returns = st.returns[1:]
		a.spawn(s)
	}
}

func (a *Arena) spawn(s int) {
	spawn := a.m.Spawns[s]
	def, ok := a.data.Mob(spawn.Mob)
	if !ok {
		return
	}
	a.nextMob++
	mb := &mob{id: a.nextMob, spawn: s, def: def, hp: def.HP, home: nav.Point(spawn.At), homeR: spawn.Radius}
	at, ok := a.spotIn(mb)
	if !ok {
		at = mb.home
	}
	a.place(mb, at)
	a.spawns[s].alive++
}

// SpawnBoss raises a field boss at its spot's centre, to wander its circle and never respawn, and returns its mob ID.
func (a *Arena) SpawnBoss(def gamedata.Mob, spot gamedata.BossSpot) uint64 {
	a.nextMob++
	a.place(&mob{id: a.nextMob, spawn: noSpawn, def: def, hp: def.HP, home: nav.Point(spot.At), homeR: spot.Radius}, nav.Point(spot.At))
	return a.nextMob
}

// place stands mb on the ground at at, facing a random way, at its own pace.
func (a *Arena) place(mb *mob, at nav.Point) {
	mb.params = a.data.Movement()
	mb.params.Radius, mb.params.Height = mb.def.Radius, mb.def.Height
	mb.x, mb.z = at.X, at.Z
	mb.y = a.groundAt(at.X, at.Z, math.Inf(-1), mb.params.Step)
	mb.yaw = float32(float64(2*math.Pi*a.roll.Float64()) - math.Pi)
	mb.changed, mb.attack, mb.ready = true, -1, make([]uint64, len(mb.def.Attacks))
	mb.pace = 1 + float64(a.data.MobRules().Patrol.SpeedJitter*(float64(2*a.roll.Float64())-1))
	a.mobs = append(a.mobs, mb)
}

// groundAt is the ground under (x, z) for feet at y: the terrain, or a solid's top no more than step above them or the terrain.
func (a *Arena) groundAt(x, z, y, step float64) float64 {
	g := a.m.Geometry
	return sim.Ground(x, z, max(y, g.Terrain.Height(x, z))+step, g)
}

// Strike is a player's hit, a basic attack or a skill, from where they stand along their aim.
type Strike struct {
	By    kernel.CharacterID
	Shape gamedata.Shape
	Most  int // the most mobs it hits
	Power model.Attacker
	Aim   model.Aim
}

// Blow is what a strike did to one mob it reached: its damage, the HP it took, which overkill leaves out, and the mob if it died.
type Blow struct {
	Damage model.Damage
	Dealt  int
	Killed *Kill
}

// Kill is a mob a strike killed, as its drops and its EXP need it.
type Kill struct {
	Mob     gamedata.Mob
	X, Y, Z float64
}

// Strike resolves a hit against each mob its shape reaches, nearest first; a killed mob leaves the map and waits to respawn.
func (a *Arena) Strike(s Strike) []Blow {
	bodies := make([]model.Body, 0, len(a.mobs))
	for _, mb := range a.mobs {
		bodies = append(bodies, mb.body())
	}
	reached := model.Reached(s.Aim, s.Shape, bodies, s.Most)
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
				b.Killed = &Kill{Mob: mb.def, X: mb.x, Y: mb.y, Z: mb.z}
			} else {
				a.react(mb, s.Aim)
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

// Changes are the tick's moved, turned, spawned or hurt mobs and those removed, each once; it keeps the frame for BlockersAt.
func (a *Arena) Changes() (changed []model.MobView, removed []uint64) {
	for _, mb := range a.mobs {
		if mb.changed {
			changed = append(changed, mb.view())
		}
	}
	a.Unwatched()
	return changed, slices.Clone(a.removed)
}

// Unwatched ends a tick no player saw as Changes does, without its views: it keeps the frame for BlockersAt and clears the marks.
func (a *Arena) Unwatched() {
	kept := &a.history[a.tick%History]
	kept.tick, kept.blockers = a.tick, kept.blockers[:0]
	for _, mb := range a.mobs {
		kept.blockers = append(kept.blockers, mb.blocker())
		mb.changed = false
	}
}

// BlockersAt are the living mobs as tick t's frame left them, as sim blockers; none past History ticks or before it's sent.
func (a *Arena) BlockersAt(t uint64) []sim.Blocker {
	if kept := a.history[t%History]; kept.tick == t {
		return kept.blockers
	}
	return nil
}
