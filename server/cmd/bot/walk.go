package main

import (
	"cmp"
	"math"
	"slices"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
)

const (
	waypointReach = 0.3              // metres from a path's waypoint the bot turns to the next
	repathEvery   = time.Second      // a path is found again this often, or sooner when its end moves
	repathMoved   = 1.0              // metres its end may move before the path is found again
	stallMoved    = 0.05             // metres a bot pressing a way must move in stallFor decisions not to have stalled
	stallFor      = 4                // decisions
	inTheWayGap   = 0.5              // metres between bodies at which a mob ahead stands in the bot's way
	inTheWayCone  = 0.5              // the cosine of the widest angle off the bot's way a mob in its way stands at, 60°
	reachShare    = 0.7              // of an attack's reach a bot goes in to, so a step either way still hits
	pathMargin    = 0.25             // metres a path keeps clear beyond the body's radius where it can, as eight-way keys stray off its line
	asideFor      = 2                // decisions a stalled bot steps aside
	giveUpFor     = 30 * time.Second // a bot leaves a mob it found no way to this long
	bossMargin    = 3.0              // metres past a field boss's sight a bot keeps its hunt and its way
)

// keysToward are the keys whose way is nearest (dx, dz), x east and z north: one key, or two for a diagonal, each within 22.5° of the way.
func keysToward(dx, dz float64) *clientv1.Input {
	l := math.Hypot(dx, dz)
	if l == 0 {
		return &clientv1.Input{}
	}
	s := l * math.Sin(math.Pi/8)
	return &clientv1.Input{Right: dx > s, Left: dx < -s, Up: dz > s, Down: dz < -s}
}

// yawTo is the aim from one point to another: radians from north toward east.
func yawTo(from, to nav.Point) float32 {
	return float32(math.Atan2(to.X-from.X, to.Z-from.Z))
}

// walker follows nav paths, turning each waypoint into keys; it finds the path again when its end moves, a second on, or when the bot stalls. With no way found it walks straight at its end for a second before looking again, as a search that fails covers the whole map.
type walker struct {
	finder nav.Finder
	path   []nav.Point
	mapID  string
	to     nav.Point
	found  time.Time
	none   bool // the last search found no way
	stalls int  // since it last reached a waypoint
	aside  int  // decisions left stepping aside
}

// keys are the keys that walk a body of radius r at from on m toward to, along a path round what's in the way. A stall is met with a jump the first time, a step aside the next, each other step the other side, in turn.
func (w *walker) keys(m *gamedata.Map, r float64, from, to nav.Point, now time.Time, stalled bool) *clientv1.Input {
	if w.mapID != m.ID || nav.Distance(w.to, to) > repathMoved || now.Sub(w.found) > repathEvery || (!w.none && (stalled || len(w.path) == 0)) {
		leash := math.Hypot(m.Geometry.SizeX, m.Geometry.SizeZ)
		w.path = w.finder.Path(m.Grid(), from, to, r+pathMargin, from, leash)
		if len(w.path) == 0 {
			w.path = w.finder.Path(m.Grid(), from, to, r, from, leash)
		}
		if n := len(w.path); n > 0 {
			w.path[n-1] = to
		}
		w.mapID, w.to, w.found, w.none = m.ID, to, now, len(w.path) == 0
	}
	for len(w.path) > 1 && nav.Distance(from, w.path[0]) < waypointReach {
		w.path, w.stalls = w.path[1:], 0
	}
	next := to
	if len(w.path) > 0 {
		next = w.path[0]
	}
	dx, dz := next.X-from.X, next.Z-from.Z
	if stalled {
		w.stalls++
		if w.stalls%2 == 1 {
			in := keysToward(dx, dz)
			in.Jump = true
			return in
		}
		w.aside = asideFor
	}
	if w.aside > 0 {
		w.aside--
		side := float64(w.stalls%4 - 1) // 1 at the second stall, -1 at the fourth
		return keysToward(-dz*side, dx*side)
	}
	return keysToward(dx, dz)
}

// lost reports whether the last search found no way to the walker's end.
func (w *walker) lost() bool { return w.none }

// stall finds a bot stuck against something: stallFor decisions pressing one way on the ground that move it less than stallMoved, told once and then counted again. Frames in the air or standing never count, so updates that arrive in bursts don't count as one, and a new way starts the count, as its first steps take a round trip to show.
type stall struct {
	at    nav.Point
	way   [2]int
	count int
}

// stalled takes a decision's position and the input held into it, and reports whether the bot has just stalled.
func (s *stall) stalled(at nav.Point, onGround bool, held *clientv1.Input) bool {
	way := wayOf(held)
	if !onGround || way == ([2]int{}) || way != s.way || nav.Distance(at, s.at) >= stallMoved {
		s.count, s.at, s.way = 0, at, way
		return false
	}
	s.count++
	if s.count < stallFor {
		return false
	}
	s.count = 0
	return true
}

// wayOf is the way an input's keys press, x east and z north, each -1, 0 or 1.
func wayOf(in *clientv1.Input) [2]int {
	key := func(plus, minus bool) int {
		switch {
		case plus && !minus:
			return 1
		case minus && !plus:
			return -1
		}
		return 0
	}
	return [2]int{key(in.GetRight(), in.GetLeft()), key(in.GetUp(), in.GetDown())}
}

// at is where the bot stands on the ground's plane.
func (b *bot) at() nav.Point { return nav.Point{X: b.x, Z: b.z} }

// mapNow is the map the bot is on, nil before its Welcome.
func (b *bot) mapNow() *gamedata.Map {
	m, ok := b.data.Map(b.mapID)
	if !ok {
		return nil
	}
	return m
}

// walkTo walks toward to till within metres of it; true once there.
func (b *bot) walkTo(to nav.Point, within float64) bool {
	m := b.mapNow()
	if m == nil || nav.Distance(b.at(), to) <= within {
		b.input(&clientv1.Input{})
		return true
	}
	stalled := b.stuck.stalled(b.at(), b.onGround, b.held)
	if stalled {
		b.count(func(s *stats) { s.stalls++ })
	}
	b.input(b.walk.keys(m, b.data.Movement().Radius, b.at(), to, time.Now(), stalled))
	return false
}

// inTheWay is a living mob at the bot's height whose body is within inTheWayGap of the bot's, within 60° of the way it presses: the world holds a player pressing into it.
func (b *bot) inTheWay() *clientv1.MobView {
	dx, dz := 0.0, 0.0
	if b.held.GetRight() {
		dx++
	}
	if b.held.GetLeft() {
		dx--
	}
	if b.held.GetUp() {
		dz++
	}
	if b.held.GetDown() {
		dz--
	}
	if dx == 0 && dz == 0 {
		return nil
	}
	mv := b.data.Movement()
	for _, m := range b.mobs {
		def, ok := b.data.Mob(m.GetType())
		if !ok || m.GetHp() <= 0 || m.GetY() >= b.y+mv.Height || b.y >= m.GetY()+def.Height {
			continue
		}
		ox, oz := m.GetX()-b.x, m.GetZ()-b.z
		gap := math.Hypot(ox, oz) - mv.Radius - def.Radius
		if gap < inTheWayGap && ox*dx+oz*dz > inTheWayCone*math.Hypot(ox, oz)*math.Hypot(dx, dz) {
			return m
		}
	}
	return nil
}

// reach is how near, centre to centre, a bot goes to a mob of radius r: well inside its basic attack's shape.
func (b *bot) reach(r float64) float64 {
	c, ok := b.data.Class(b.class)
	if !ok {
		return 1
	}
	s := c.Attack.Shape
	if s.Kind == gamedata.ShotShape {
		return s.Range * reachShare
	}
	return s.Radius*reachShare + r
}

// approach walks within reach of a mob; true once there.
func (b *bot) approach(m *clientv1.MobView) bool {
	r := 0.5
	if def, ok := b.data.Mob(m.GetType()); ok {
		r = def.Radius
	}
	return b.walkTo(nav.Point{X: m.GetX(), Z: m.GetZ()}, b.reach(r))
}

// strike uses the main skill at a mob, else attacks it, aimed at it.
func (b *bot) strike(m *clientv1.MobView) {
	aim := yawTo(b.at(), nav.Point{X: m.GetX(), Z: m.GetZ()})
	if !b.useSkill(aim) {
		b.command(&clientv1.Attack{Aim: aim})
	}
}

// travel walks into the portal that starts the way to a map and goes through; off the bots' maps it takes the nearest portal.
func (b *bot) travel(to string) {
	p, ok := b.world.portalTo(b.mapID, to)
	if !ok {
		m := b.mapNow()
		if m == nil || len(m.Portals) == 0 {
			return
		}
		p = slices.MinFunc(m.Portals, func(a, c gamedata.Portal) int {
			return cmp.Compare(nav.Distance(b.at(), point(a.At)), nav.Distance(b.at(), point(c.At)))
		})
	}
	if !b.walkTo(point(p.At), p.Radius/2) {
		return
	}
	if time.Since(b.portalAt) > time.Second {
		b.command(&clientv1.EnterPortal{})
		b.portalAt = time.Now()
	}
}

func point(p gamedata.Point) nav.Point { return nav.Point{X: p.X, Z: p.Z} }

// talkWithin is how near, across the ground, a bot walks to an NPC to talk: well inside the talk reach, which counts from the NPC to the bot's middle.
func (b *bot) talkWithin() float64 { return b.data.TalkReach() / 2 }

// nearToTalk walks to an NPC; true once it stands on the ground within talkWithin, as a jump can carry its middle out of reach.
func (b *bot) nearToTalk(n gamedata.NPC) bool {
	return b.walkTo(point(n.At), b.talkWithin()) && b.onGround
}

// talkTo walks to an NPC and talks to it, at most once a talkGap.
func (b *bot) talkTo(n gamedata.NPC) {
	if b.nearToTalk(n) && time.Since(b.lastTalk[n.ID]) > talkGap {
		b.command(&clientv1.Talk{NpcId: n.ID})
		b.lastTalk[n.ID] = time.Now()
	}
}

// hunt goes after the nearest prey on a spot it has a way to, clear of a field boss, as a player does, or walks to a point rolled in the spot while none is there; it gives the mob once it is in reach to strike.
func (b *bot) hunt(s spot, prey []string) *clientv1.MobView {
	skip := func(m *clientv1.MobView) bool {
		return m.GetId() == b.boss || time.Now().Before(b.gaveUp[m.GetId()]) || b.nearBoss(b.mapID, nav.Point{X: m.GetX(), Z: m.GetZ()})
	}
	if m := nearest(b.mobs, s, prey, b.at(), skip); m != nil {
		if b.approach(m) {
			return m
		}
		if b.walk.lost() {
			b.gaveUp[m.GetId()] = time.Now().Add(giveUpFor)
		}
		return nil
	}
	if !s.holds(b.roam) || b.nearBoss(b.mapID, b.roam) || b.walkTo(b.roam, 1) || b.walk.lost() {
		b.roam = b.roll(s)
	}
	return nil
}

// roll is a point in a spot's circle the bot fits at, clear of a field boss, the circle's centre if a few tries find none.
func (b *bot) roll(s spot) nav.Point {
	m := b.mapNow()
	for range 8 {
		a, d := b.rng.Float64()*2*math.Pi, s.radius*math.Sqrt(b.rng.Float64())
		p := nav.Point{X: s.at.X + d*math.Sin(a), Z: s.at.Z + d*math.Cos(a)}
		if m != nil && m.Grid().Fits(p, b.data.Movement().Radius) && !b.nearBoss(b.mapID, p) {
			return p
		}
	}
	return s.at
}

// bossAt is where the field boss up on a map stands, as far as the bot knows, and how far round it a bot keeps clear: its mob's place on the bot's map, else its circle, which it wanders. A bot waiting the boss out goes by its circle, so the boss wandering back and forth doesn't turn it back and forth.
func (b *bot) bossAt(mapID string) (nav.Point, float64, bool) {
	if !b.bossUp(mapID) {
		return nav.Point{}, 0, false
	}
	clear := b.data.MobRules().Sight + bossMargin
	if m, ok := b.mobs[b.boss]; ok && mapID == b.mapID && b.waiting != mapID {
		return nav.Point{X: m.GetX(), Z: m.GetZ()}, clear, true
	}
	m, ok := b.data.Map(mapID)
	if !ok || m.Boss == nil {
		return nav.Point{}, 0, false
	}
	return point(m.Boss.At), m.Boss.Radius + clear, true
}

// nearBoss reports whether p on a map is where its field boss could see a bot, or near it: a square, as far either way in x and z, so further clear than sight's circle.
func (b *bot) nearBoss(mapID string, p nav.Point) bool {
	at, r, ok := b.bossAt(mapID)
	return ok && max(math.Abs(p.X-at.X), math.Abs(p.Z-at.Z)) < r
}

// bossCovers reports whether a spot lies wholly where a map's field boss could see a bot, or near it; a spot partly clear is hunted in its clear part.
func (b *bot) bossCovers(mapID string, s spot) bool {
	at, r, ok := b.bossAt(mapID)
	return ok && math.Abs(s.at.X-at.X)+s.radius < r && math.Abs(s.at.Z-at.Z)+s.radius < r
}

// bossBlocks is the map whose field boss keeps the bot off a goal: one near a portal on its way there or where it comes out, near the NPC it talks to, or all over the spot it hunts.
func (b *bot) bossBlocks(g goal) (string, bool) {
	to := b.mapOf(g)
	for at := b.mapID; at != to; {
		p, ok := b.world.portalTo(at, to)
		if !ok {
			break
		}
		if b.nearBoss(at, point(p.At)) {
			return at, true
		}
		if out, ok := b.world.arrival(p); ok && b.nearBoss(p.TargetMap, out) {
			return p.TargetMap, true
		}
		at = p.TargetMap
	}
	switch g.kind {
	case talkGoal:
		if b.nearBoss(to, point(g.npc.At)) {
			return to, true
		}
	case huntGoal:
		if b.bossCovers(to, g.spot) {
			return to, true
		}
	}
	return "", false
}

// townNPCs are the NPCs of a map in the order a visit talks to them: from the one nearest where players appear, each next the nearest left.
func townNPCs(data *gamedata.Data, m *gamedata.Map) []gamedata.NPC {
	var left []gamedata.NPC
	for _, n := range data.NPCs() {
		if n.MapID == m.ID {
			left = append(left, n)
		}
	}
	slices.SortFunc(left, func(a, c gamedata.NPC) int { return cmp.Compare(a.ID, c.ID) })
	out := make([]gamedata.NPC, 0, len(left))
	for from := point(m.PlayerSpawn); len(left) > 0; {
		k := 0
		for i, n := range left {
			if nav.Distance(from, point(n.At)) < nav.Distance(from, point(left[k].At)) {
				k = i
			}
		}
		out = append(out, left[k])
		from = point(left[k].At)
		left = slices.Delete(left, k, k+1)
	}
	return out
}

// visitNPCs walks to each NPC on the map in turn and talks to it, then stands a second for the dialog and its answers; false once every one has been visited.
func (b *bot) visitNPCs() bool {
	m := b.mapNow()
	if m == nil {
		return false
	}
	npcs := townNPCs(b.data, m)
	if b.stop >= len(npcs) {
		return false
	}
	if !b.talked.IsZero() {
		if time.Since(b.talked) > time.Second {
			b.stop, b.talked = b.stop+1, time.Time{}
		}
		return true
	}
	if n := npcs[b.stop]; b.nearToTalk(n) {
		b.command(&clientv1.Talk{NpcId: n.ID})
		b.talked = time.Now()
	}
	return true
}
