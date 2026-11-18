package instance

import (
	"math"
	"slices"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/sim"
)

// enemiesIn are p's enemies on the map still up, with their bodies, in join order: a scan, not the arena's interest cells, since it runs only on a match's map, 30 fighters at most.
func (i *Instance) enemiesIn(p *player) ([]*player, []model.Body) {
	var out []*player
	var bodies []model.Body
	for _, id := range i.order {
		if q := i.players[id]; !q.dead && q.HP > 0 && i.enemies(p.ID, id) { // one fallen this tick takes no more
			out, bodies = append(out, q), append(bodies, i.bodyOf(q))
		}
	}
	return out, bodies
}

// reachedEnemies are the enemies s's shape reaches, up to its most, the lock first when reached, then nearest first.
func (i *Instance) reachedEnemies(p *player, lock model.EntityRef, s mobs.Strike) []*player {
	enemies, bodies := i.enemiesIn(p)
	reached := model.Reached(s.Aim, s.Shape, bodies, len(bodies))
	if k := slices.IndexFunc(reached, func(idx int) bool { return enemies[idx].ID == lock.Player }); k > 0 {
		first := reached[k]
		reached = slices.Insert(slices.Delete(reached, k, k+1), 0, first)
	}
	reached = reached[:min(s.Most, len(reached))]
	out := make([]*player, 0, len(reached))
	for _, idx := range reached {
		out = append(out, enemies[idx])
	}
	return out
}

// strikePlayers lands p's strike on the enemies it reaches; it reports whether it reached one.
func (i *Instance) strikePlayers(p *player, lock model.EntityRef, s mobs.Strike) bool {
	hit := i.reachedEnemies(p, lock, s)
	for _, t := range hit {
		i.hitPlayer(t, p, s, true)
	}
	return len(hit) > 0
}

// afflictPlayers lands a status skill on the enemies it reaches: no damage, but a dodge or a block stops it as it would a hit.
func (i *Instance) afflictPlayers(p *player, lock model.EntityRef, s mobs.Strike) {
	for _, t := range i.reachedEnemies(p, lock, s) {
		i.hitPlayer(t, p, s, false)
	}
}

// blowWay is the way a blow from by to t travels across the ground, a unit vector; straight along by's aim when they stand on one spot.
func (i *Instance) blowWay(by, t *player) (float64, float64) {
	dx, dz := t.state.X-by.state.X, t.state.Z-by.state.Z
	if d := math.Hypot(dx, dz); d > 1e-9 {
		return dx / d, dz / d
	}
	return model.HeadingOf(by.aim)
}

// hitPlayer is one player's strike landing on another, damage when it deals some: a dodge passes it, a block from the front stops it, and else it lands through shields to HP with a blink, its push a shove with no hop, and what it inflicts. A player's hit sets no hit guard, so a combo lands whole.
func (i *Instance) hitPlayer(t, by *player, s mobs.Strike, damages bool) {
	tick := i.tick
	src, dst := model.EntityRef{Player: by.ID}, model.EntityRef{Player: t.ID}
	dx, dz := i.blowWay(by, t)
	if t.dodging(tick) {
		i.events = append(i.events, model.Damage{Tick: tick, Source: src, Target: dst, Dodge: true})
		t.dodged, t.dodgedSeq = true, t.lastSeq
		return
	}
	if t.guards(tick, dx, dz, i.data.Block().Front) {
		i.events = append(i.events, model.Damage{Tick: tick, Source: src, Target: dst, Block: true})
		i.blockEnergy(t)
		t.blocked, t.blockedSeq = true, t.lastSeq
		return
	}
	if damages {
		h := model.Resolve(s.Power, t.statuses.Defending(model.PlayerDefender(t.derived)), i.data.StatRules().Hit, i.roll)
		dealt := model.Damage{Tick: tick, Source: src, Target: dst, Crit: h.Crit, Miss: h.Miss}
		if h.Miss {
			i.events = append(i.events, dealt)
			return
		}
		dealt.Amount = i.scaled(float64(h.Damage) * t.statuses.Shatter(tick))
		dealt.Shielded = dealt.Amount - t.shielded(dealt.Amount)
		i.events = append(i.events, dealt)
		t.HP -= dealt.Amount - dealt.Shielded
	}
	t.invulnUntil = tick + i.ticksFor(i.data.MobRules().HitGuard) // the blink only: nextHit stays
	t.changed = true
	if s.Push > 0 {
		t.state = sim.Push(t.state, dx*s.Push, dz*s.Push, i.geometry(), i.params)
		t.moved = true
	}
	if t.HP <= 0 {
		i.kill(t)
		return
	}
	t.statuses.Wake(tick)
	for _, in := range s.Inflicts {
		if in.Status.Group != gamedata.Aggro { // taunt and lose sight are mobs' alone
			i.inflict(t, in)
		}
	}
	i.restrain(t, tick+1) // as a mob's blow does: the Delta's one correction for the hit carries the hold and slow it puts on
}
