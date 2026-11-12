package instance

import (
	"math"
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
	thousandths      = 1000
	respawnDelay     = 3 * time.Second
	respawnHPPercent = 50
	knockbackX       = 16.0 // px away from the mob
	knockbackVY      = -200.0
	fallMargin       = 64.0 // px below the map's bottom, two tiles, past which a falling player is taken back to its spawn
)

// RespawnHP is the HP a dead character comes back with.
func RespawnHP(maxHP int) int { return max(1, maxHP*respawnHPPercent/100) }

func (i *Instance) ticksFor(d time.Duration) uint64 { return model.TicksIn(d, i.tuning.TickInterval) }

// regen gives each living player a second of HP and MP regen, in towns too; MP is not in the Delta, so its rise goes to the player in StatsChanged.
func (i *Instance) regen() {
	for _, id := range i.order {
		p := i.players[id]
		if p.dead {
			continue
		}
		if regained(&p.HP, &p.hpCarry, p.derived.HPRegen, p.derived.MaxHP) {
			p.changed = true
		}
		if regained(&p.MP, &p.mpCarry, p.derived.MPRegen, p.derived.MaxMP) {
			p.private = append(p.private, model.StatsChanged{Tick: i.tick, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
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
		length := model.AttackLengthTicks(a.Length, p.derived.AttackSpeed, i.tuning.TickInterval)
		i.begin(p, attackAction, i.tick, length, a.Timing, true, func() { i.strike(p, a.Shape(), 1, power) })
	}
}

// strike hits up to most mobs the shape reaches from the player, tells the map the damage, and drops, shares EXP and counts each kill.
func (i *Instance) strike(p *player, shape gamedata.Shape, most int, power model.Attacker) {
	blows := i.arena.Strike(mobs.Strike{
		By: p.ID, Shape: shape, Most: most, Power: power,
		X: p.state.X, Y: p.state.Y, Facing: p.state.Facing, Height: i.params.Height,
	})
	for _, b := range blows {
		i.events = append(i.events, b.Damage)
		if k := b.Killed; k != nil {
			i.ground.Roll(i.claim(p, k.Mob, k.X, k.Y))
			i.shareExp(p, k.Mob.Exp)
			i.countKill(p, k.Mob.ID)
		}
	}
}

// contactDamage rolls a mob's touch on a player at most once a touch guard. A hit hurts, knocks back and briefly protects them; a MISS only shows.
func (i *Instance) contactDamage() {
	if i.m.Kind == gamedata.Town {
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if p.dead || p.nextTouch > i.tick {
			continue
		}
		dealt, mobX, touched := i.arena.Touch(p.ID, i.playerBox(p), model.PlayerDefender(p.derived))
		if !touched {
			continue
		}
		i.events = append(i.events, dealt)
		p.nextTouch = i.tick + i.ticksFor(i.data.TouchGuard()) // a hit shows it as invulnerability
		if dealt.Miss {
			continue // a dodge keeps the player where they are, without the hurt blink
		}
		p.invulnUntil = p.nextTouch
		p.changed = true // the Delta carries the invulnerability's start
		dir := 1.0
		if p.state.X < mobX {
			dir = -1
		}
		p.state = knockedBack(p.state, dir*knockbackX, i.m.Geometry.Solids, i.params)
		p.state.VY = knockbackVY
		p.state.OnGround = false
		p.state.Ground = sim.NoPlatform
		p.moved = true
		p.HP -= dealt.Amount
		if p.HP <= 0 {
			i.kill(p)
		}
	}
}

// knockedBack is s moved dx sideways, stopped at the face of the first solid beside it: sim.Step only pushes a player out of a solid they walk into.
func knockedBack(s sim.State, dx float64, solids []sim.Rect, p sim.Params) sim.State {
	half := p.Width / 2
	x := s.X + dx
	for _, r := range solids {
		if s.Y-p.Height >= r.Y+r.H || s.Y <= r.Y {
			continue // above or below the player's box
		}
		switch {
		case dx > 0 && r.X > s.X:
			x = min(x, r.X-half)
		case dx < 0 && r.X+r.W < s.X:
			x = max(x, r.X+r.W+half)
		}
	}
	s.X = x
	return s
}

// fellOut reports whether s has fallen past the bottom of m, out of the world, or lost its position to NaN.
func fellOut(s sim.State, m *gamedata.Map) bool {
	return s.Y > m.Height+fallMargin || math.IsNaN(s.X) || math.IsNaN(s.Y)
}

// rescue takes a player who fell out of the map back to its spawn, alive and with nothing lost; the Delta corrects the client.
func (i *Instance) rescue(p *player) {
	i.log.Warn("a player fell out of the map; it took them back to its spawn", "character_id", p.ID, "map_id", i.m.ID, "x", p.state.X, "y", p.state.Y)
	p.state = standing(i.m.PlayerSpawn.X, i.m.PlayerSpawn.Y)
}

// standing is a character at rest at (x, y), as a join, a respawn or a rescue places one.
func standing(x, y float64) sim.State {
	return sim.State{X: x, Y: y, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
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
		if !p.dead || p.deadUntil > i.tick || p.itemCalls > 0 && i.m.Town != i.m.ID {
			continue // an item call in flight settles here before the respawn hands the player off
		}
		p.dead = false
		p.HP = RespawnHP(p.derived.MaxHP)
		p.changed = true
		if i.m.Town == i.m.ID { // a town revives its own dead
			x, y := i.m.PlayerSpawn.X, i.m.PlayerSpawn.Y
			i.events = append(i.events, model.Respawned{Tick: i.tick, ID: p.ID, MapID: i.m.ID, X: x, Y: y, HP: p.HP})
			p.state = standing(x, y)
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
