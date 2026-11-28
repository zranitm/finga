// Package mobs runs a map's mobs, their spawns, wandering, chases and attacks, and players' strikes on them (3d-world.md section 7).
package mobs

import (
	"cmp"
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
)

const noSpawn = -1 // a boss's spawn: it never respawns

// History is how many ticks of mob bodies the arena keeps, the oldest an input's view may name.
const History = 8

// framed is the living mobs as one tick's frame left them, as the sim's blockers.
type framed struct {
	tick     uint64
	ids      []uint64 // each blocker's mob
	blockers []sim.Blocker
	blocking sim.Blockers // blockers bucketed, for the steps that read them
}

// mob wanders its spawn circle until it sees a player, walks to its spot beside them and attacks, and walks home past its leash.
type mob struct {
	id      uint64
	slot    int // its index in the arena's mobs
	cell    int // the interest cell its centre stands in
	spawn   int // index into the map's spawns; noSpawn for a boss
	def     gamedata.Mob
	params  sim.Params // the sim's, with its own radius and height: how it walks into solids and players
	hp      int
	x, y, z float64 // feet centre
	yaw     float32
	home    nav.Point // its spawn circle's centre
	homeR   float64   // and radius
	changed bool
	told    bool // a frame has ended since it spawned

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
	wentAt     uint64             // wandering or going home, the last tick it stood progressStep from where it stood the time before
	wentFrom   nav.Point          // where it stood then
	homing     bool               // led past its leash: walking home, blind to players until it's back in its circle
	calm       bool               // a standing boss nobody has hit since it was last home: it sees no one
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
	statuses      model.Statuses
	push          pushing // a skill's push under way
}

type spawnState struct {
	alive   int
	returns []uint64 // ticks at which a replacement spawns
}

func (mb *mob) at() nav.Point { return nav.Point{X: mb.x, Z: mb.z} }

func (mb *mob) body() model.Body {
	return model.Body{X: mb.x, Y: mb.y, Z: mb.z, Radius: mb.def.Radius, Height: mb.def.Height}
}

// framed is mb as the wire gives it, at the centimetre, so a mover's step meets what the page holds.
func (mb *mob) framed() sim.Blocker {
	x, y, z := quant.Metres(quant.Position(mb.x)), quant.Metres(quant.Position(mb.y)), quant.Metres(quant.Position(mb.z))
	return sim.Blocker{X: x, Z: z, Radius: mb.def.Radius, Bottom: y, Top: y + mb.def.Height}
}

func (mb *mob) blocker() sim.Blocker {
	return sim.Blocker{X: mb.x, Z: mb.z, Radius: mb.def.Radius, Bottom: mb.y, Top: mb.y + mb.def.Height}
}

func (mb *mob) view() model.MobView {
	return model.MobView{
		ID: mb.id, Type: mb.def.ID, X: mb.x, Y: mb.y, Z: mb.z, Yaw: mb.yaw, HP: mb.hp, MaxHP: mb.def.HP, Pose: mb.pose,
		Statuses: mb.statuses.Shown(),
	}
}

// speed is the mob's m/s at its own pace.
func (mb *mob) speed() float64 { return mb.def.Speed * mb.pace * mb.statuses.MoveSpeed() }

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
	removed []uint64        // this tick's
	unseen  []model.MobView // this tick's removed that no frame showed, as they were
	hit     gamedata.HitCurve
	history [History]framed // by tick mod History

	started  []model.MobAttacked // this tick's
	landings []Landing           // this tick's
	dots     []Blow              // this tick's damage over time
	shots    []*shot
	portals  []sim.Blocker // the map's portals, which a mob never steps into, so a player can always leave by one
	blockers []sim.Blocker // the portals and this tick's living players, whom a mob's body stops at
	standing []sim.Blocker // the living mobs where they stand, reused
	passing  []sim.Blocker // the blockers but the player a mob steps out of, reused

	// The sets above bucketed, so a body's move reads only those near it.
	portalBlocking, blocking, standingBlocking, passingBlocking sim.Blockers

	players  []Body                     // this tick's living players
	playerAt map[kernel.CharacterID]int // their indices by ID
	near     playerCells                // and by cell
	bodies   mobCells                   // the living mobs by cell

	finder   nav.Finder
	searches int         // this tick's path searches left
	held     []place     // the places a claim keeps clear of, reused
	angles   []float64   // the turns off its own angle a claim tries, reused
	pushes   []nav.Point // by mob, the drift spread adds up for it, reused
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
	a := &Arena{m: d.Map, data: d.Data, roll: d.Roll, interval: t.TickInterval, spawns: make([]spawnState, len(d.Map.Spawns)), hit: rules.Hit, playerAt: map[kernel.CharacterID]int{}}
	c := newCells(d.Data.Interest(), d.Map.Geometry.SizeX, d.Map.Geometry.SizeZ)
	a.near = playerCells{cells: c, in: make([][]int32, c.nx*c.nz)}
	a.bodies = mobCells{cells: c, in: make([][]*mob, c.nx*c.nz)}
	for _, p := range d.Map.Portals {
		a.portals = append(a.portals, sim.Blocker{X: p.At.X, Z: p.At.Z, Radius: p.Radius, Bottom: math.Inf(-1), Top: math.Inf(1)})
	}
	a.portalBlocking.Set(a.portals, d.Map.Geometry.SizeX, d.Map.Geometry.SizeZ)
	for s, spawn := range d.Map.Spawns {
		for range spawn.Max {
			a.spawn(s)
		}
	}
	return a
}

// Step starts tick n: respawns, spits in flight, the mobs' statuses, each mob's turn among the living players, from one mob further on each tick so none waits behind the others for its path searches, then mobs that overlap drift apart, and a mob inside a player steps out. A mob whose interest cell awake doesn't hold sleeps (large-regions.md section 5); nil holds every cell.
func (a *Arena) Step(n uint64, players []Body, awake []bool) {
	a.start(n, players)
	a.runStatuses()
	for k := range a.mobs {
		mb := a.mobs[(k+int(n%uint64(len(a.mobs))))%len(a.mobs)]
		if awake == nil || awake[mb.cell] {
			a.turn(mb, players)
		} else {
			a.doze(mb)
		}
	}
	a.spread()
	a.yield(players)
}

// yield moves each mob a player stands in out to their side, stopped as a walk is: to the nearest point touching them that no other mob or player is on, within a mob's width, else straight out. A player steps against the mobs their view drew, so a mob closing in since, or a hop landing on them, can end up inside them; the mob gives way, as the client predicts only the player.
func (a *Arena) yield(players []Body) {
	for _, mb := range a.mobs {
		for i := a.overlapped(mb, 0); i >= 0; i = a.overlapped(mb, i+1) { // asked again from where it stands after each step out
			to := a.touching(mb, players, i)
			a.passing = append(append(a.passing[:0], a.blockers[:len(a.portals)+i]...), a.blockers[len(a.portals)+i+1:]...) // all but p, whom it leaves
			a.passingBlocking.Set(a.passing, a.m.Geometry.SizeX, a.m.Geometry.SizeZ)
			a.move(mb, to.X-mb.x, to.Z-mb.z, &a.passingBlocking)
		}
	}
}

// overlapped is the first of the tick's players from index from whom mb, as it stands, is inside; -1 for none.
func (a *Arena) overlapped(mb *mob, from int) int {
	near := a.playersNear(mb.x, mb.z, mb.x, mb.z, mb.def.Radius+a.near.reach)
	for i, ok := near.next(); ok; i, ok = near.next() {
		if int(i) >= from && overlaps(mb, mb.at(), a.players[i]) {
			return int(i)
		}
	}
	return -1
}

// touching is where mb, inside players[i], goes to touch them: the point beside them nearest its own, round from straight out, that no other mob or player is on, if it's within a mob's width; straight out if not.
func (a *Arena) touching(mb *mob, players []Body, i int) nav.Point {
	p := players[i]
	reach := mb.def.Radius + p.Radius
	out := math.Atan2(mb.x-p.X, mb.z-p.Z)
	if mb.x == p.X && mb.z == p.Z {
		out = float64(mb.yaw) + math.Pi // on their spot: it backs away the way it faces
	}
	beside := func(turn float64) nav.Point {
		sin, cos := math.Sincos(out + turn)
		return nav.Point{X: p.X + float64(reach*sin), Z: p.Z + float64(reach*cos)}
	}
	straight := beside(0)
	for k := range touchTries {
		for _, side := range [2]float64{1, -1} {
			at := beside(float64(side * float64(k) * touchStep)) // rounded alone, so no platform fuses it into the angle's sum
			if nav.Distance(mb.at(), at) > 2*mb.def.Radius {
				return straight // each turn further moves it further
			}
			if a.room(mb, at, players, p) {
				return at
			}
		}
	}
	return straight
}

// room reports whether mb could stand at at by p without overlapping another player or another mob, but for those still inside p, which step out after.
func (a *Arena) room(mb *mob, at nav.Point, players []Body, p Body) bool {
	mobs := a.mobsNear(at.X, at.Z, at.X, at.Z, mb.def.Radius+a.bodies.reach)
	for o, ok := mobs.next(); ok; o, ok = mobs.next() {
		if o != mb && !overlaps(o, o.at(), p) && nav.Distance(at, o.at()) < mb.def.Radius+o.def.Radius-1e-6 && o.y < mb.y+mb.def.Height && mb.y < o.y+o.def.Height {
			return false
		}
	}
	near := a.playersNear(at.X, at.Z, at.X, at.Z, mb.def.Radius+a.near.reach)
	for i, ok := near.next(); ok; i, ok = near.next() {
		if q := players[i]; q.ID != p.ID && overlaps(mb, at, q) {
			return false
		}
	}
	return a.offPortals(at, mb.def.Radius) && a.m.Grid().Fits(at, mb.def.Radius)
}

// overlaps reports whether mb's body standing at at would be inside player p's.
func overlaps(mb *mob, at nav.Point, p Body) bool {
	return nav.Distance(at, p.at()) < mb.def.Radius+p.Radius-1e-9 && p.Y < mb.y+mb.def.Height && mb.y < p.Y+p.Height
}

// Blockers are the living mobs where they stand now, as sim blockers: what a knockback stops at. The next call reuses them.
func (a *Arena) Blockers() *sim.Blockers {
	a.standing = a.standing[:0]
	for _, mb := range a.mobs {
		a.standing = append(a.standing, mb.blocker())
	}
	a.standingBlocking.Set(a.standing, a.m.Geometry.SizeX, a.m.Geometry.SizeZ)
	return &a.standingBlocking
}

// Rest starts tick n on a map nobody is on: respawns, spits in flight and statuses go on, and every mob sleeps, so an empty map costs next to nothing.
func (a *Arena) Rest(n uint64) {
	a.start(n, nil)
	a.runStatuses()
	for _, mb := range a.mobs {
		a.doze(mb)
	}
}

// doze is a sleeping mob's turn: a push, an act and a walk home under way go on, and one after someone, or a standing boss still awake, gives up as with nobody left to fight; but it looks for nobody and doesn't wander.
func (a *Arena) doze(mb *mob) {
	mb.near, mb.waiting, mb.claimFor = false, false, kernel.CharacterID{}
	a.settle(mb)
	if a.pushed(mb) {
		return
	}
	if mb.attack >= 0 {
		a.advance(mb, nil)
		return
	}
	if !mb.homing && (mb.target != kernel.CharacterID{} || mb.spawn == noSpawn && !mb.calm) {
		a.giveUp(mb) // it walks home, and a boss calms there
	}
	if mb.homing && mb.pauseUntil <= a.tick && !mb.held() {
		a.goHome(mb)
	}
}

func (a *Arena) start(n uint64, players []Body) {
	a.tick, a.searches = n, searches
	a.removed, a.started, a.landings, a.dots = a.removed[:0], a.started[:0], a.landings[:0], a.dots[:0]
	a.players = players
	a.near.set(players)
	clear(a.playerAt)
	for i, p := range players {
		a.playerAt[p.ID] = i
	}
	a.blockers = append(a.blockers[:0], a.portals...)
	for _, p := range players {
		a.blockers = append(a.blockers, sim.Blocker{X: p.X, Z: p.Z, Radius: p.Radius, Bottom: p.Y, Top: p.Y + p.Height})
	}
	a.blocking.Set(a.blockers, a.m.Geometry.SizeX, a.m.Geometry.SizeZ)
	for s := range a.m.Spawns {
		a.respawn(s)
	}
	a.fly()
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
	a.place(&mob{id: a.nextMob, spawn: noSpawn, def: def, hp: def.HP, home: nav.Point(spot.At), homeR: spot.Radius, calm: true}, nav.Point(spot.At))
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
	mb.slot = len(a.mobs)
	a.mobs = append(a.mobs, mb)
	a.bodies.add(mb)
}

// groundAt is the ground under (x, z) for feet at y: the terrain, or a solid's top no more than step above them or the terrain.
func (a *Arena) groundAt(x, z, y, step float64) float64 {
	g := a.m.Geometry
	return sim.Ground(x, z, max(y, g.Terrain.Height(x, z))+step, g)
}

// Strike is a player's hit, a basic attack or a skill, from where they stand along their aim.
type Strike struct {
	By       kernel.CharacterID
	Shape    gamedata.Shape
	Most     int    // the most mobs it hits
	Target   uint64 // the mob it hits first when the shape reaches it, then the nearest; 0, a dead or an unknown id is none
	Power    model.Attacker
	Aim      model.Aim
	Inflicts []model.Infliction // on each mob it lands on
	Push     float64            // metres each mob it lands on is pushed away
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

// Strike resolves a hit against each mob its shape reaches, its target first when reached, then nearest first, through the mob's statuses: a freeze shatters, a sleeper wakes, and one still alive reacts and takes what the strike inflicts. A killed mob leaves the map and waits to respawn.
func (a *Arena) Strike(s Strike) []Blow {
	hit := a.reached(s)
	blows := make([]Blow, 0, len(hit))
	for _, mb := range hit {
		mb.calm = false // any blow wakes a standing boss, even from someone it can't see
		if mb.provokable(s.By) {
			mb.provoker, mb.provokedUntil = s.By, a.afterBlow(a.data.MobRules().Provoke) // a MISS provokes too
		}
		h := model.Resolve(s.Power, mb.statuses.Defending(model.MobDefender(mb.def)), a.hit, a.roll)
		b := Blow{Damage: model.Damage{Tick: a.tick, Source: model.EntityRef{Player: s.By}, Target: model.EntityRef{Mob: mb.id}, Amount: h.Damage, Crit: h.Crit, Miss: h.Miss}}
		if !h.Miss {
			b.Damage.Amount = a.shattered(mb, h.Damage)
			b.Dealt = min(b.Damage.Amount, mb.hp)
			mb.hp -= b.Damage.Amount
			mb.changed = true
			if mb.hp <= 0 {
				a.remove(mb)
				b.Killed = &Kill{Mob: mb.def, X: mb.x, Y: mb.y, Z: mb.z}
			} else {
				mb.statuses.Wake(a.tick)
				a.react(mb, s.Aim, s.By, s.Push)
				for _, in := range s.Inflicts {
					a.inflict(mb, in)
				}
			}
		}
		blows = append(blows, b)
	}
	return blows
}

// reached are the mobs s's shape reaches, up to its most, its target first when reached, then nearest first.
func (a *Arena) reached(s Strike) []*mob {
	var near []*mob
	var bodies []model.Body
	mobs := a.mobsNear(s.Aim.X, s.Aim.Z, s.Aim.X, s.Aim.Z, max(s.Shape.Radius, s.Shape.Range+s.Shape.Width/2)+a.bodies.reach) // past it, no shape meets a body
	for mb, ok := mobs.next(); ok; mb, ok = mobs.next() {
		near, bodies = append(near, mb), append(bodies, mb.body())
	}
	reached := model.Reached(s.Aim, s.Shape, bodies, len(bodies))
	if k := slices.IndexFunc(reached, func(idx int) bool { return near[idx].id == s.Target }); k > 0 {
		target := reached[k]
		reached = slices.Insert(slices.Delete(reached, k, k+1), 0, target)
	}
	reached = reached[:min(s.Most, len(reached))]
	hit := make([]*mob, 0, len(reached))
	for _, idx := range reached {
		hit = append(hit, near[idx]) // before a kill removes any of them
	}
	return hit
}

// At is where the living mob id stands across the ground, and whether it's on the map.
func (a *Arena) At(id uint64) (gamedata.Point, bool) {
	for _, mb := range a.mobs {
		if mb.id == id {
			return gamedata.Point{X: mb.x, Z: mb.z}, true
		}
	}
	return gamedata.Point{}, false
}

// Controlled is whether the living mob id holds one of statuses at this tick, or ended one within slack ticks.
func (a *Arena) Controlled(id uint64, statuses []string, slack uint64) bool {
	for _, mb := range a.mobs {
		if mb.id == id {
			return mb.statuses.Holds(statuses, a.tick, slack)
		}
	}
	return false
}

// remove takes mb off the map with its attacks: what lands this tick, and its spits in flight.
func (a *Arena) remove(mb *mob) {
	a.mobs = slices.Delete(a.mobs, mb.slot, mb.slot+1)
	for _, o := range a.mobs[mb.slot:] {
		o.slot--
	}
	a.bodies.remove(mb)
	a.landings = slices.DeleteFunc(a.landings, func(l Landing) bool { return l.Mob == mb.id })
	a.shots = slices.DeleteFunc(a.shots, func(s *shot) bool { return s.mob == mb })
	a.removed = append(a.removed, mb.id)
	if !mb.told {
		a.unseen = append(a.unseen, mb.view()) // gone on the tick it spawned: told as it was, so a client can draw its hit
	}
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

// View is mob id as it stands; false once it's gone.
func (a *Arena) View(id uint64) (model.MobView, bool) {
	k, ok := slices.BinarySearchFunc(a.mobs, id, func(mb *mob, id uint64) int { return cmp.Compare(mb.id, id) })
	if !ok {
		return model.MobView{}, false
	}
	return a.mobs[k].view(), true
}

// Changes are the tick's moved, turned, spawned or hurt mobs and those removed, each once, and the removed that no frame showed as they were; it keeps the frame for BlockersAt.
func (a *Arena) Changes() (changed []model.MobView, removed []uint64) {
	for _, mb := range a.mobs {
		if mb.changed {
			changed = append(changed, mb.view())
		}
	}
	changed = append(changed, a.unseen...)
	a.Unwatched()
	return changed, slices.Clone(a.removed)
}

// Unwatched ends a tick no player saw as Changes does, without its views: it keeps the frame for BlockersAt and clears the marks.
func (a *Arena) Unwatched() {
	kept := &a.history[a.tick%History]
	kept.tick, kept.ids, kept.blockers = a.tick, kept.ids[:0], kept.blockers[:0]
	for _, mb := range a.mobs {
		kept.ids, kept.blockers = append(kept.ids, mb.id), append(kept.blockers, mb.framed())
		mb.changed, mb.told = false, true
	}
	kept.blocking.Set(kept.blockers, a.m.Geometry.SizeX, a.m.Geometry.SizeZ)
	a.unseen = a.unseen[:0]
}

// BodyAt is mob id's body as tick t's frame left it, as a sim blocker; false if that frame isn't kept or held no such mob.
func (a *Arena) BodyAt(t, id uint64) (sim.Blocker, bool) {
	kept := &a.history[t%History]
	if kept.tick != t {
		return sim.Blocker{}, false
	}
	if k := slices.Index(kept.ids, id); k >= 0 {
		return kept.blockers[k], true
	}
	return sim.Blocker{}, false
}

// BodiesAt are the living mobs as tick t's frame left them, as sim blockers; nil past History ticks or before it's sent. The list is the arena's: it holds till the frame is kept over.
func (a *Arena) BodiesAt(t uint64) []sim.Blocker {
	if kept := &a.history[t%History]; kept.tick == t {
		return kept.blockers
	}
	return nil
}

// BlockersAt are the living mobs as tick t's frame left them, as sim blockers; nil past History ticks or before it's sent.
func (a *Arena) BlockersAt(t uint64) *sim.Blockers {
	if kept := &a.history[t%History]; kept.tick == t {
		return &kept.blocking
	}
	return nil
}
