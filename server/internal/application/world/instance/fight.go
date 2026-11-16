package instance

import (
	"math"
	"slices"
	"time"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

const (
	thousandths      = 1000
	respawnDelay     = 3 * time.Second
	respawnHPPercent = 50
	knockback        = 0.5  // metres a mob's hit pushes a player away from it
	knockbackVY      = 6.25 // m/s it hops them up
)

// RespawnHP is the HP a dead character comes back with.
func RespawnHP(maxHP int) int { return max(1, maxHP*respawnHPPercent/100) }

func (i *Instance) ticksFor(d time.Duration) uint64 { return model.TicksIn(d, i.tuning.TickInterval) }

// regen gives each living player a second of HP regen, in towns too.
func (i *Instance) regen() {
	for _, id := range i.order {
		p := i.players[id]
		if p.dead {
			continue
		}
		if regained(&p.HP, &p.hpCarry, p.derived.HPRegen, p.derived.MaxHP) {
			p.changed = true
		}
	}
}

// regained adds a second of perSecond to v, held to most, carrying the fraction in thousandths so small rates add up exactly; the carry empties at the maximum.
func regained(v, carry *int, perSecond float64, most int) bool {
	if *v >= most {
		*carry = 0
		return false
	}
	*carry += int(math.Round(perSecond * thousandths))
	gain := *carry / thousandths
	*carry %= thousandths
	if gain == 0 {
		return false
	}
	*v = min(*v+gain, most)
	if *v == most {
		*carry = 0
	}
	return true
}

// resolveAttacks swings each queued attack once its player's next attack may start, so one sent a little early is not lost; a town has none.
func (i *Instance) resolveAttacks() {
	town := i.m.Kind == gamedata.Town
	for _, id := range i.order {
		p := i.players[id]
		if !p.attackQueued || p.action.nextStart(attackAction) > i.tick {
			continue
		}
		p.attackQueued = false
		if town || p.dead {
			continue
		}
		a, power := p.class.Attack, model.PlayerAttacker(p.derived, p.class.Attack.Damage)
		length := model.TicksIn(a.Length, i.tuning.TickInterval)
		p.aimAt(p.attackAim)
		i.events = append(i.events, model.AttackUsed{Tick: i.tick, Attacker: p.ID, Length: length, Aim: p.aim}) // before begin: a hit at share 0 lands now, and a watcher times its Damage by this attack
		i.begin(p, attackAction, i.tick, length, a.Timing, true, func() {
			if i.strike(p, a.Shape, 1, power) {
				i.gainEnergy(p) // a basic attack that reaches a mob, hit or MISS
			}
		})
	}
}

// strike hits up to most mobs the shape reaches from the player along its aim, tells the map the damage, tallies what the boss took, and drops, shares EXP and counts each kill; a beaten boss pays out by damage. It reports whether it reached a mob.
func (i *Instance) strike(p *player, shape gamedata.Shape, most int, power model.Attacker) bool {
	blows := i.arena.Strike(mobs.Strike{
		By: p.ID, Shape: shape, Most: most, Power: power,
		Aim: model.Aim{X: p.state.X, Y: p.state.Y, Z: p.state.Z, Yaw: float64(p.aim)},
	})
	for _, b := range blows {
		i.events = append(i.events, b.Damage)
		boss := i.boss != nil && b.Damage.Target.Mob == i.boss.id
		if boss {
			i.boss.dealt[p.ID] += b.Dealt
		}
		switch k := b.Killed; {
		case k == nil:
		case boss:
			i.bossFell(p, k)
		default:
			i.ground.Roll(i.claim(p, k.Mob, k.X, k.Y, k.Z))
			i.shareExp(p, k.Mob.Exp)
			i.countKill(p, k.Mob.ID)
			i.countTowardBoss()
		}
	}
	return len(blows) > 0
}

// gainEnergy gives p a basic attack's energy, up to the most a player holds.
func (i *Instance) gainEnergy(p *player) {
	e := i.data.Energy()
	p.Energy = min(p.Energy+e.PerAttack, e.Max)
}

// mobHits lands this tick's mob attacks on the players outside their hit guard.
func (i *Instance) mobHits() {
	guard := i.ticksFor(i.data.MobRules().HitGuard)
	for _, l := range i.arena.Landings() {
		for _, h := range l.Hits {
			if p, ok := i.players[h.Player]; ok && !p.dead && p.nextHit <= i.tick {
				i.mobHit(p, i.arena.Roll(l, p.ID, model.PlayerDefender(p.derived)), h, guard)
			}
		}
	}
}

// mobHit lands a mob's blow on p, which pushes them away from the mob along the hit's way, stopped by solids and the mobs, and hops them up.
func (i *Instance) mobHit(p *player, dealt model.Damage, h mobs.Hit, guard uint64) {
	i.events = append(i.events, dealt)
	p.nextHit = i.tick + guard // a hit shows it as invulnerability
	if dealt.Miss {
		return // a dodge keeps the player where they are, without the hurt blink
	}
	p.invulnUntil = p.nextHit
	p.changed = true // the Delta carries the invulnerability's start
	g := i.m.Geometry
	g.Blockers = i.arena.Blockers() // so a blow never knocks a player into another mob
	p.state = sim.Push(p.state, h.DX*knockback, h.DZ*knockback, g, i.params)
	p.state.VY = knockbackVY
	p.state.OnGround = false
	p.moved = true
	p.HP -= dealt.Amount
	if p.HP <= 0 {
		i.kill(p)
	}
}

// rescue takes a player who fell out of the map back to its spawn, alive and with nothing lost; the Delta corrects the client.
func (i *Instance) rescue(p *player) {
	i.log.Warn("a player fell out of the map; it took them back to its spawn", "character_id", p.ID, "map_id", i.m.ID, "x", p.state.X, "y", p.state.Y, "z", p.state.Z)
	p.state = i.standing(i.m.PlayerSpawn.X, 0, i.m.PlayerSpawn.Z, float32(i.m.SpawnYaw)) // the action runs on, as through a knockback
}

func (i *Instance) kill(p *player) {
	p.HP = 0
	p.dead = true
	p.deadUntil = i.tick + i.ticksFor(respawnDelay)
	p.held = sim.Input{}
	p.pending = p.pending[:0]
	if len(p.buffs) > 0 { // buffs end at death
		clear(p.buffs)
		i.restat(p)
	}
	p.action, p.windUps = action{}, nil // the action ends, and a hit still to land with it
	p.Energy, p.spentAhead = 0, nil     // and the energy goes
	progress := progression.DeathPenalty(i.data, progression.Progress{Level: p.Level, Exp: p.Exp}, i.m.Kind == gamedata.Town)
	if progress.Exp != p.Exp {
		p.private = append(p.private, model.ExpGained{Tick: i.tick, Amount: progress.Exp - p.Exp, Exp: progress.Exp})
	}
	p.Exp = progress.Exp
	p.changed = true
	i.events = append(i.events, model.Died{Tick: i.tick, ID: p.ID, X: p.state.X, Y: p.state.Y, Z: p.state.Z})
}

// deathsAndRespawns revives the dead at the town spawn and returns those handed off.
func (i *Instance) deathsAndRespawns() []kernel.CharacterID {
	var left []kernel.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		if !p.dead || p.deadUntil > i.tick || p.itemCalls > 0 && i.m.Town != i.m.ID {
			continue // an item call in flight settles here before the respawn hands the player off
		}
		p.dead = false
		p.HP = RespawnHP(p.derived.MaxHP)
		p.changed = true
		if i.m.Town == i.m.ID { // a town revives its own dead
			yaw := float32(i.m.SpawnYaw)
			p.state = i.standing(i.m.PlayerSpawn.X, 0, i.m.PlayerSpawn.Z, yaw)
			i.events = append(i.events, model.Respawned{Tick: i.tick, ID: p.ID, MapID: i.m.ID, X: p.state.X, Y: p.state.Y, Z: p.state.Z, HP: p.HP, Yaw: yaw})
			i.tellParty(p, i.m.ID)
			p.moved = true
			continue
		}
		// The player leaves before the broadcast, so tell them directly.
		town := i.town()
		yaw := float32(town.SpawnYaw)
		at := standOn(town, i.params, town.PlayerSpawn.X, 0, town.PlayerSpawn.Z, yaw)
		respawned := model.Respawned{Tick: i.tick, ID: p.ID, MapID: town.ID, X: at.X, Y: at.Y, Z: at.Z, HP: p.HP, Yaw: yaw}
		i.events = append(i.events, respawned)
		p.sink.Send(respawned)
		i.handOff(p, town, at.X, at.Z, yaw)
		left = append(left, id)
	}
	return left
}
