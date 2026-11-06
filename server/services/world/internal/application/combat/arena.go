// Package combat runs a map's mobs: their spawns and patrols, the players' attacks on them, and their touch.
package combat

import (
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

const mobPause = time.Second // at a platform edge

// mob patrols the platform it spawned on: walk, pause at an edge, turn.
type mob struct {
	id         uint64
	spawn      int // index into the map's spawn areas
	def        gamedata.Mob
	hp         int
	x, y       float64 // feet centre
	facing     int
	minX, maxX float64
	pauseUntil uint64
	changed    bool
}

type spawnState struct {
	alive   int
	returns []uint64 // ticks at which a replacement spawns
}

func (mb *mob) box() model.Rect {
	return model.Rect{X: mb.x - mb.def.Width/2, Y: mb.y - mb.def.Height, W: mb.def.Width, H: mb.def.Height}
}

func (mb *mob) view() model.MobView {
	return model.MobView{ID: mb.id, Type: mb.def.ID, X: mb.x, Y: mb.y, Facing: mb.facing, HP: mb.hp, MaxHP: mb.def.HP}
}

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
}

// NewArena spawns every area's mobs on m, so the map is full before its first tick; roll is the map's random source.
func NewArena(m *gamedata.Map, data *gamedata.Data, roll model.Roll, interval time.Duration) *Arena {
	a := &Arena{m: m, data: data, roll: roll, interval: interval, spawns: make([]spawnState, len(m.Spawns))}
	for s, area := range m.Spawns {
		for range area.Max {
			a.spawn(s)
		}
	}
	return a
}

// Step starts tick n: the mobs whose time came respawn, and the rest walk their patrols.
func (a *Arena) Step(n uint64) {
	a.tick = n
	a.removed = a.removed[:0]
	for s := range a.m.Spawns {
		a.respawn(s)
	}
	dt := a.interval.Seconds()
	for _, mb := range a.mobs {
		if mb.pauseUntil > a.tick || mb.maxX <= mb.minX {
			continue
		}
		mb.x += float64(mb.facing) * mb.def.Speed * dt
		switch {
		case mb.x >= mb.maxX:
			mb.x, mb.facing, mb.pauseUntil = mb.maxX, -1, a.tick+model.TicksIn(mobPause, a.interval)
		case mb.x <= mb.minX:
			mb.x, mb.facing, mb.pauseUntil = mb.minX, 1, a.tick+model.TicksIn(mobPause, a.interval)
		}
		mb.changed = true
	}
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
	mb := &mob{id: a.nextMob, spawn: s, def: def, hp: def.HP, x: x, y: y, facing: 1, changed: true}
	if a.roll.Float64() < 0.5 {
		mb.facing = -1
	}
	mb.minX, mb.maxX = a.patrolBounds(x, y, half, area)
	a.mobs = append(a.mobs, mb)
	a.spawns[s].alive++
}

// patrolBounds is the walkable x range: the ground under the mob within its area, else the area.
func (a *Arena) patrolBounds(x, y, half float64, area gamedata.Spawn) (minX, maxX float64) {
	ground := append(append([]model.Rect(nil), a.m.Geometry.Solids...), a.m.Geometry.Platforms...)
	for _, r := range ground {
		if math.Abs(r.Y-y) < 1 && x >= r.X && x <= r.X+r.W {
			return max(r.X, area.Area.X) + half, min(r.X+r.W, area.Area.X+area.Area.W) - half
		}
	}
	return area.Area.X + half, area.Area.X + area.Area.W - half
}

// Strike is a player's basic attack, from where they stand the way they face.
type Strike struct {
	By     kernel.CharacterID
	Attack gamedata.Attack
	Power  model.Attacker
	X, Y   float64 // the attacker's feet centre
	Facing int
	Height float64 // the attacker's; a shot flies at half of it
}

// Blow is what a strike that reached a mob did: its damage, and the mob if it died.
type Blow struct {
	Damage model.Damage
	Killed *Kill
}

// Kill is a mob a strike killed, as its drops and its EXP need it.
type Kill struct {
	Mob  gamedata.Mob
	X, Y float64
}

// Strike resolves an attack against the mob it reaches, if any; a killed mob leaves the map and waits to respawn.
func (a *Arena) Strike(s Strike) (Blow, bool) {
	targets := make([]model.Target, 0, len(a.mobs))
	for _, mb := range a.mobs {
		targets = append(targets, model.Target{Box: mb.box()})
	}
	var idx int
	var reached bool
	switch s.Attack.Kind {
	case gamedata.Melee:
		box := model.MeleeBox(s.X, s.Y, s.Facing, s.Attack.Width, s.Attack.Height)
		idx, reached = model.NearestInBox(s.X, s.Y, box, targets)
	case gamedata.Ranged:
		idx, reached = model.FirstInLine(s.X, s.Y-s.Height/2, s.Facing, s.Attack.Range, targets)
	}
	if !reached {
		return Blow{}, false
	}
	mb := a.mobs[idx]
	h := model.Resolve(s.Power, model.Defender{Defense: float64(mb.def.Defense), Evasion: float64(mb.def.Evasion)}, a.roll)
	b := Blow{Damage: model.Damage{Tick: a.tick, Source: model.EntityRef{Player: s.By}, Target: model.EntityRef{Mob: mb.id}, Amount: h.Damage, Crit: h.Crit, Miss: h.Miss}}
	if h.Miss {
		return b, true
	}
	mb.hp -= h.Damage
	mb.changed = true
	if mb.hp <= 0 {
		a.remove(mb)
		b.Killed = &Kill{Mob: mb.def, X: mb.x, Y: mb.y}
	}
	return b, true
}

func (a *Arena) remove(mb *mob) {
	a.mobs = slices.DeleteFunc(a.mobs, func(m *mob) bool { return m == mb })
	a.removed = append(a.removed, mb.id)
	st := &a.spawns[mb.spawn]
	st.alive--
	st.returns = append(st.returns, a.tick+model.TicksIn(a.m.Spawns[mb.spawn].Respawn, a.interval))
}

// Touch is the hit of the first mob box overlaps, on player id defending with d; mobX is which way it knocks them.
func (a *Arena) Touch(id kernel.CharacterID, box model.Rect, d model.Defender) (dealt model.Damage, mobX float64, touched bool) {
	for _, mb := range a.mobs {
		if !box.Overlaps(mb.box()) {
			continue
		}
		h := model.Resolve(model.Attacker{AttackPower: float64(mb.def.Attack), Accuracy: float64(mb.def.Accuracy)}, d, a.roll)
		return model.Damage{Tick: a.tick, Source: model.EntityRef{Mob: mb.id}, Target: model.EntityRef{Player: id}, Amount: h.Damage, Crit: h.Crit, Miss: h.Miss}, mb.x, true
	}
	return model.Damage{}, 0, false
}

// Views are every mob, as a Snapshot shows them.
func (a *Arena) Views() []model.MobView {
	views := make([]model.MobView, 0, len(a.mobs))
	for _, mb := range a.mobs {
		views = append(views, mb.view())
	}
	return views
}

// Changes are the mobs that moved, spawned or were hurt this tick, and those removed; each is reported once.
func (a *Arena) Changes() (changed []model.MobView, removed []uint64) {
	for _, mb := range a.mobs {
		if mb.changed {
			changed = append(changed, mb.view())
			mb.changed = false
		}
	}
	return changed, slices.Clone(a.removed)
}
