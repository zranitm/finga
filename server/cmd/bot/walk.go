package main

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/internal/shared/steer"
)

const (
	inTheWayGap  = 0.5              // metres between bodies at which a mob ahead stands in the bot's way
	inTheWayCone = 0.5              // the cosine of the widest angle off the bot's way a mob in its way stands at, 60°
	reachShare   = 0.7              // of an attack's reach a bot goes in to, so a step either way still hits
	giveUpFor    = 30 * time.Second // a bot leaves a mob it found no way to this long
	bossClear    = 15.0             // metres from a field boss a bot keeps its prey and its hunt: past the boss's body and the widest area round the bot with its reach to its prey, or the longest shot that goes on past its first mob, so no blow of its wakes the boss
)

// clientKeys is a client's Input holding in's keys.
func clientKeys(in sim.Input) *clientv1.Input {
	return &clientv1.Input{Left: in.Left, Right: in.Right, Up: in.Up, Down: in.Down, Jump: in.Jump}
}

// simKeys is the keys a client's Input holds.
func simKeys(in *clientv1.Input) sim.Input {
	return sim.Input{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Down: in.GetDown(), Jump: in.GetJump()}
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
	stalled := b.stuck.Stalled(b.at(), b.onGround, simKeys(b.held))
	if stalled {
		b.count(func(s *stats) { s.stalls++ })
	}
	b.input(clientKeys(b.walk.Keys(m, b.data.Movement().Radius, b.at(), to, time.Since(b.born), stalled)))
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
	aim := steer.YawTo(b.at(), nav.Point{X: m.GetX(), Z: m.GetZ()})
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
		if b.walk.Lost() {
			b.gaveUp[m.GetId()] = time.Now().Add(giveUpFor)
		}
		return nil
	}
	if !s.holds(b.roam) || b.nearBoss(b.mapID, b.roam) || b.walkTo(b.roam, 1) || b.walk.Lost() {
		b.roam = b.roll(s)
	}
	return nil
}

// lootFrom is how far a bot goes for a pouch of its own; one farther is left to run out.
const lootFrom = 20.0

// loot walks to the bot's nearest pouch within lootFrom and takes all it holds, as Take all does, and every other pouch in reach with it; false with none to go to.
func (b *bot) loot() bool {
	var near *clientv1.PouchView
	for id, p := range b.pouches {
		d := nav.Distance(b.at(), nav.Point{X: p.GetX(), Z: p.GetZ()})
		if !b.leftPouches[id] && d <= lootFrom && (near == nil || d < nav.Distance(b.at(), nav.Point{X: near.GetX(), Z: near.GetZ()})) {
			near = p
		}
	}
	if near == nil {
		return false
	}
	if !b.walkTo(nav.Point{X: near.GetX(), Z: near.GetZ()}, b.data.Loot().Reach/2) {
		if b.walk.Lost() {
			b.leftPouches[near.GetId()] = true
		}
		return true
	}
	if !b.taking {
		b.taking = true
		b.command(b.takeInReach())
	}
	return true
}

// takeInReach takes every item of every pouch it doesn't leave within half the reach, as Take all does.
func (b *bot) takeInReach() *clientv1.TakeLoot {
	take := &clientv1.TakeLoot{}
	for _, id := range slices.Sorted(maps.Keys(b.pouches)) {
		p := b.pouches[id]
		if !b.leftPouches[id] && nav.Distance(b.at(), nav.Point{X: p.GetX(), Z: p.GetZ()}) <= b.data.Loot().Reach/2 {
			for _, it := range p.GetItems() {
				take.Picks = append(take.Picks, &clientv1.LootPick{Pouch: id, Item: it.GetItem()})
			}
		}
	}
	return take
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

// bossAt is where the field boss up on a map stands, as far as the bot knows, and how far round it a bot keeps clear: its mob's place on the bot's map, else its circle, which it wanders.
func (b *bot) bossAt(mapID string) (nav.Point, float64, bool) {
	if !b.bossUp(mapID) {
		return nav.Point{}, 0, false
	}
	if m, ok := b.mobs[b.boss]; ok && mapID == b.mapID {
		return nav.Point{X: m.GetX(), Z: m.GetZ()}, bossClear, true
	}
	m, ok := b.data.Map(mapID)
	if !ok || m.Boss == nil {
		return nav.Point{}, 0, false
	}
	return point(m.Boss.At), m.Boss.Radius + bossClear, true
}

// nearBoss reports whether p on a map is where a bot's blows could reach its field boss, which fights only once hit: a square, as far either way in x and z, so further clear than a circle.
func (b *bot) nearBoss(mapID string, p nav.Point) bool {
	at, r, ok := b.bossAt(mapID)
	return ok && max(math.Abs(p.X-at.X), math.Abs(p.Z-at.Z)) < r
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
