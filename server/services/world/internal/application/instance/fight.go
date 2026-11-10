package instance

import (
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
)

const (
	respawnDelay     = 3 * time.Second
	respawnHPPercent = 50
	invulnerability  = time.Second // after contact damage
	knockbackX       = 16.0        // px away from the mob
	knockbackVY      = -250.0
)

// RespawnHP is the HP a dead character comes back with.
func RespawnHP(maxHP int) int { return max(1, maxHP*respawnHPPercent/100) }

func (i *Instance) ticksFor(d time.Duration) uint64 { return model.TicksIn(d, i.tuning.TickInterval) }

// resolveAttacks swings each queued attack once its cooldown ends, so one sent a little early is not lost; a town has none.
func (i *Instance) resolveAttacks() {
	town := i.m.Kind == gamedata.Town
	for _, id := range i.order {
		p := i.players[id]
		if !p.attackQueued || p.cooldownUntil > i.tick {
			continue
		}
		p.attackQueued = false
		if town || p.dead {
			continue
		}
		p.cooldownUntil = i.tick + i.ticksFor(p.class.Attack.Cooldown)
		blow, reached := i.arena.Strike(mobs.Strike{
			By: p.ID, Attack: p.class.Attack, Power: attackerOf(p.derived),
			X: p.state.X, Y: p.state.Y, Facing: p.state.Facing, Height: i.params.Height,
		})
		if !reached {
			continue
		}
		i.events = append(i.events, blow.Damage)
		if k := blow.Killed; k != nil {
			i.ground.Roll(i.claim(p, k.Mob, k.X, k.Y))
			i.shareExp(p, k.Mob.Exp)
		}
	}
}

// contactDamage hurts, knocks back and briefly protects a player touching a mob.
func (i *Instance) contactDamage() {
	if i.m.Kind == gamedata.Town {
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if p.dead || p.invulnUntil > i.tick {
			continue
		}
		dealt, mobX, touched := i.arena.Touch(p.ID, i.playerBox(p), defenderOf(p.derived))
		if !touched {
			continue
		}
		i.events = append(i.events, dealt)
		p.invulnUntil = i.tick + i.ticksFor(invulnerability)
		p.changed = true // the Delta carries the invulnerability's start
		dir := 1.0
		if p.state.X < mobX {
			dir = -1
		}
		p.state.X += dir * knockbackX
		p.state.VY = knockbackVY
		p.state.OnGround = false
		p.state.Ground = -1
		p.moved = true
		if dealt.Miss {
			continue
		}
		p.HP -= dealt.Amount
		if p.HP <= 0 {
			i.kill(p)
		}
	}
}

func (i *Instance) kill(p *player) {
	p.HP = 0
	p.dead = true
	p.deadUntil = i.tick + i.ticksFor(respawnDelay)
	p.held = sim.Input{}
	p.pending = p.pending[:0]
	progress := progression.DeathPenalty(i.data, progression.Progress{Level: p.Level, Exp: p.Exp}, i.m.Kind == gamedata.Town)
	if progress.Exp != p.Exp {
		p.private = append(p.private, model.ExpGained{Tick: i.tick, Amount: progress.Exp - p.Exp, Exp: progress.Exp})
	}
	p.Exp = progress.Exp
	p.changed = true
	i.events = append(i.events, model.Died{Tick: i.tick, ID: p.ID, X: p.state.X, Y: p.state.Y})
}

// deathsAndRespawns revives the dead at the town spawn and returns those handed off.
func (i *Instance) deathsAndRespawns() []kernel.CharacterID {
	var left []kernel.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		if !p.dead || p.deadUntil > i.tick || p.equipping > 0 && i.m.Town != i.m.ID {
			continue // an equip in flight settles here before the respawn hands the player off
		}
		p.dead = false
		p.HP = RespawnHP(p.derived.MaxHP)
		p.changed = true
		if i.m.Town == i.m.ID { // a town revives its own dead
			x, y := i.m.PlayerSpawn.X, i.m.PlayerSpawn.Y
			i.events = append(i.events, model.Respawned{Tick: i.tick, ID: p.ID, MapID: i.m.ID, X: x, Y: y, HP: p.HP})
			p.state = sim.State{X: x, Y: y, Facing: 1, Ground: -1, DropPlatform: -1}
			i.tellParty(p, i.m.ID)
			p.moved = true
			continue
		}
		// The player leaves before the broadcast, so tell them directly.
		town := i.town()
		x, y := town.PlayerSpawn.X, town.PlayerSpawn.Y
		respawned := model.Respawned{Tick: i.tick, ID: p.ID, MapID: town.ID, X: x, Y: y, HP: p.HP}
		i.events = append(i.events, respawned)
		p.sink.Send(respawned)
		i.handOff(p, town, x, y)
		left = append(left, id)
	}
	return left
}

func attackerOf(d progression.Derived) model.Attacker {
	return model.Attacker{AttackPower: d.PhysicalAttack, Accuracy: d.Accuracy, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration}
}

func defenderOf(d progression.Derived) model.Defender {
	return model.Defender{Defense: d.PhysicalDefense, Evasion: d.Evasion}
}
