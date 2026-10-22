package world

import (
	"math"
	"slices"

	"finga/internal/application/character"
	"finga/internal/application/combat"
	"finga/internal/application/gamedata"
	"finga/internal/application/world/sim"
	"finga/internal/model"
)

// mob is one live mob: its type, HP, position, and patrol state. Mobs
// patrol the platform they spawned on: walk, pause at an edge, turn.
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

// spawnState is a spawn area's bookkeeping: how many are alive and when
// the missing ones return.
type spawnState struct {
	alive   int
	returns []uint64 // ticks at which a replacement spawns
}

const mobPause = 20 // ticks a mob pauses at an edge (1 s at 20 Hz)

func (mb *mob) box() combat.Rect {
	return combat.Rect{X: mb.x - mb.def.Width/2, Y: mb.y - mb.def.Height, W: mb.def.Width, H: mb.def.Height}
}

func (mb *mob) view() MobView {
	return MobView{ID: mb.id, Type: mb.def.ID, X: mb.x, Y: mb.y, Facing: mb.facing, HP: mb.hp, MaxHP: mb.def.HP}
}

func (mb *mob) attacker() combat.Attacker {
	return combat.Attacker{AttackPower: float64(mb.def.Attack), Accuracy: float64(mb.def.Accuracy)}
}

func (mb *mob) defender() combat.Defender {
	return combat.Defender{Defense: float64(mb.def.Defense), Evasion: float64(mb.def.Evasion)}
}

// stepMobs spawns what is due and moves every mob one tick.
func (i *Instance) stepMobs() {
	for s := range i.m.Spawns {
		i.fillSpawn(s)
	}
	dt := i.opts.TickInterval.Seconds()
	for _, mb := range i.mobs {
		if mb.pauseUntil > i.tick || mb.maxX <= mb.minX { // paused, or nowhere to walk
			continue
		}
		mb.x += float64(mb.facing) * mb.def.Speed * dt
		switch {
		case mb.x >= mb.maxX:
			mb.x, mb.facing, mb.pauseUntil = mb.maxX, -1, i.tick+mobPause
		case mb.x <= mb.minX:
			mb.x, mb.facing, mb.pauseUntil = mb.minX, 1, i.tick+mobPause
		}
		mb.changed = true
	}
}

// fillSpawn brings a spawn area up to its maximum: on the first tick all at
// once, later one per respawn timer that has run out.
func (i *Instance) fillSpawn(s int) {
	area := i.m.Spawns[s]
	st := &i.spawns[s]
	if i.tick == 1 {
		for st.alive < area.Max {
			i.spawnMob(s)
		}
		return
	}
	for len(st.returns) > 0 && st.returns[0] <= i.tick && st.alive < area.Max {
		st.returns = st.returns[1:]
		i.spawnMob(s)
	}
}

func (i *Instance) spawnMob(s int) {
	area := i.m.Spawns[s]
	def, ok := i.data.Mob(area.Mob)
	if !ok {
		return
	}
	i.nextMob++
	half := def.Width / 2
	x := area.Area.X + half + i.roll.Float64()*math.Max(0, area.Area.W-def.Width)
	y := area.Area.Y + area.Area.H
	mb := &mob{id: i.nextMob, spawn: s, def: def, hp: def.HP, x: x, y: y, facing: 1, changed: true}
	if i.roll.Float64() < 0.5 {
		mb.facing = -1
	}
	mb.minX, mb.maxX = i.patrolBounds(x, y, half, area)
	i.mobs = append(i.mobs, mb)
	i.spawns[s].alive++
}

// patrolBounds is the x range a mob at (x, y) can walk: the ground it
// stands on within its spawn area, trimmed by half its width, or the spawn
// area alone when no ground
// is found right under it.
func (i *Instance) patrolBounds(x, y, half float64, area gamedata.Spawn) (minX, maxX float64) {
	ground := append(append([]combat.Rect(nil), rects(i.m.Geometry.Solids)...), rects(i.m.Geometry.Platforms)...)
	for _, r := range ground {
		if math.Abs(r.Y-y) < 1 && x >= r.X && x <= r.X+r.W {
			return max(r.X, area.Area.X) + half, min(r.X+r.W, area.Area.X+area.Area.W) - half
		}
	}
	return area.Area.X + half, area.Area.X + area.Area.W - half
}

func rects(in []sim.Rect) []combat.Rect {
	out := make([]combat.Rect, 0, len(in))
	for _, r := range in {
		out = append(out, combat.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	}
	return out
}

// resolveAttacks runs every attack queued this tick, in join order.
func (i *Instance) resolveAttacks() {
	if i.m.Kind == gamedata.Town { // no combat in a town (page 5)
		for _, id := range i.order {
			i.players[id].attackQueued = false
		}
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if !p.attackQueued {
			continue
		}
		p.attackQueued = false
		if p.dead || p.cooldownUntil > i.tick {
			continue
		}
		p.cooldownUntil = i.tick + i.ticksFor(p.class.Attack.Cooldown)
		targets := make([]combat.Target, 0, len(i.mobs))
		for _, mb := range i.mobs {
			targets = append(targets, combat.Target{Box: mb.box()})
		}
		var idx int
		var ok bool
		switch p.class.Attack.Kind {
		case gamedata.Melee:
			box := combat.MeleeBox(p.state.X, p.state.Y, p.state.Facing, p.class.Attack.Width, p.class.Attack.Height)
			idx, ok = combat.NearestInBox(p.state.X, p.state.Y, box, targets)
		case gamedata.Ranged:
			idx, ok = combat.FirstInLine(p.state.X, p.state.Y-i.params.Height/2, p.state.Facing, p.class.Attack.Range, targets)
		}
		if !ok {
			continue
		}
		mb := i.mobs[idx]
		hit := combat.Resolve(attackerOf(p.derived), mb.defender(), i.roll)
		i.events = append(i.events, Damage{Tick: i.tick, Source: EntityRef{Player: p.ID}, Target: EntityRef{Mob: mb.id}, Amount: hit.Damage, Crit: hit.Crit, Miss: hit.Miss})
		if hit.Miss {
			continue
		}
		mb.hp -= hit.Damage
		mb.changed = true
		if mb.hp <= 0 {
			i.killMob(mb, p)
		}
	}
}

// killMob removes the mob, schedules its replacement, and awards EXP to
// the killer. Party sharing joins in step 15, drops in step 13.
func (i *Instance) killMob(mb *mob, killer *player) {
	i.mobs = removeMob(i.mobs, mb.id)
	i.removedMobs = append(i.removedMobs, mb.id)
	st := &i.spawns[mb.spawn]
	st.alive--
	st.returns = append(st.returns, i.tick+i.ticksFor(i.m.Spawns[mb.spawn].Respawn))
	i.rollDrops(mb, killer)
	i.shareExp(killer, mb.def.Exp)
}

func removeMob(mobs []*mob, id uint64) []*mob {
	for k, mb := range mobs {
		if mb.id == id {
			return append(mobs[:k], mobs[k+1:]...)
		}
	}
	return mobs
}

// award adds EXP and resolves level-ups: stats by the class preset, HP and
// MP restored, the level saved right away through the persistence worker.
func (i *Instance) award(p *player, amount int64) {
	if amount <= 0 {
		return
	}
	a := character.AwardExp(i.data, character.Progress{Level: p.Level, Exp: p.Exp}, amount, p.Stats, character.PresetPlan(p.class))
	p.Level, p.Exp, p.Stats = a.Progress.Level, a.Progress.Exp, a.Stats
	p.private = append(p.private, ExpGained{Tick: i.tick, Amount: amount, Exp: p.Exp})
	if a.LevelsGained == 0 {
		return
	}
	formulas, _ := i.data.Formulas(p.Class)
	p.derived = character.DeriveStats(formulas, p.Stats, p.Level, character.Equipment{})
	p.HP, p.MP = p.derived.MaxHP, p.derived.MaxMP
	p.changed = true
	p.private = append(p.private, LevelUp{Tick: i.tick, ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, Exp: p.Exp, Stats: p.Stats, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
	i.tellParty(p, i.m.ID)
	if i.persist != nil {
		lu := character.LevelUp{ID: p.ID, Level: p.Level, Exp: p.Exp, Stats: p.Stats, HP: p.HP, MP: p.MP}
		select {
		case i.persist <- Job{LevelUp: &lu}:
		default:
		}
	}
}

// contactDamage hurts a player touching a mob, then knocks the player back
// and makes them invulnerable for a second.
func (i *Instance) contactDamage() {
	if i.m.Kind == gamedata.Town { // mobs in a town (none in the data) are harmless
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if p.dead || p.invulnUntil > i.tick {
			continue
		}
		box := i.playerBox(p)
		for _, mb := range i.mobs {
			if !overlaps(box, mb.box()) {
				continue
			}
			hit := combat.Resolve(mb.attacker(), defenderOf(p.derived), i.roll)
			i.events = append(i.events, Damage{Tick: i.tick, Source: EntityRef{Mob: mb.id}, Target: EntityRef{Player: p.ID}, Amount: hit.Damage, Crit: hit.Crit, Miss: hit.Miss})
			p.invulnUntil = i.tick + i.ticksFor(invulnerability)
			p.changed = true // the invulnerability starts: the Delta carries the edge (step 19)
			dir := 1.0
			if p.state.X < mb.x {
				dir = -1
			}
			p.state.X += dir * knockbackX
			p.state.VY = knockbackVY
			p.state.OnGround = false
			p.state.Ground = -1
			p.moved = true
			if !hit.Miss {
				p.HP -= hit.Damage
				p.changed = true
				if p.HP <= 0 {
					i.kill(p)
				}
			}
			break
		}
	}
}

// kill applies the death penalty (none in a town) and starts the respawn
// timer; the character can't act meanwhile.
func (i *Instance) kill(p *player) {
	p.HP = 0
	p.dead = true
	p.deadUntil = i.tick + i.ticksFor(respawnDelay)
	p.held = sim.Input{}
	p.pending = p.pending[:0]
	progress := character.DeathPenalty(i.data, character.Progress{Level: p.Level, Exp: p.Exp}, i.m.Kind == gamedata.Town)
	if progress.Exp != p.Exp {
		// The penalty is private to the dying player: a negative ExpGained with
		// the new total, sent with Died (step 19). No penalty, no message.
		p.private = append(p.private, ExpGained{Tick: i.tick, Amount: progress.Exp - p.Exp, Exp: progress.Exp})
	}
	p.Exp = progress.Exp
	p.changed = true
	i.events = append(i.events, Died{Tick: i.tick, ID: p.ID, X: p.state.X, Y: p.state.Y})
}

// deathsAndRespawns brings dead players back after the delay at the linked
// town's player spawn with half their HP. Away from that town the player
// is handed to its instance; the IDs of players who left are returned. A
// map with no reachable town (or no mover) respawns in place.
func (i *Instance) deathsAndRespawns() []model.CharacterID {
	var left []model.CharacterID
	town, hasTown := i.data.Map(i.m.Town)
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		if !p.dead || p.deadUntil > i.tick {
			continue
		}
		p.dead = false
		p.HP = max(1, p.derived.MaxHP*respawnHPPercent/100)
		p.changed = true
		if !hasTown || town.ID == i.m.ID || i.opts.Transfer == nil {
			// Back on this very map: stand at its own player spawn.
			x, y := i.m.PlayerSpawn.X, i.m.PlayerSpawn.Y
			i.events = append(i.events, Respawned{Tick: i.tick, ID: p.ID, MapID: i.m.ID, X: x, Y: y, HP: p.HP})
			p.state = sim.State{X: x, Y: y, Facing: 1, Ground: -1, DropPlatform: -1}
			i.tellParty(p, i.m.ID)
			p.moved = true
			continue
		}
		// The player hears it directly (they leave this map before the
		// broadcast), everyone else through the tick's events.
		x, y := town.PlayerSpawn.X, town.PlayerSpawn.Y
		respawned := Respawned{Tick: i.tick, ID: p.ID, MapID: town.ID, X: x, Y: y, HP: p.HP}
		i.events = append(i.events, respawned)
		p.sink.Send(respawned)
		i.handOff(p, town, x, y)
		left = append(left, id)
	}
	return left
}

func overlaps(a, b combat.Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
