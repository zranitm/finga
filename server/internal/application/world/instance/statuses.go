package instance

import (
	"maps"
	"math"
	"slices"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/sim"
)

// aimOf is where p's action of shape strikes from: where they stand, along their aim, or round their lock target for an area at it.
func (i *Instance) aimOf(p *player, shape gamedata.Shape, target uint64) model.Aim {
	aim := model.Aim{X: p.state.X, Y: p.state.Y, Z: p.state.Z, Yaw: float64(p.aim)}
	if shape.At == gamedata.AtLock {
		lock, found := i.arena.At(target)
		aim = aim.AtLock(lock, found, shape.Reach)
	}
	return aim
}

// onHits are what each of p's hits inflicts while their buffs last: each buff's on_hit at the points and trait it was used with, in skill order.
func (i *Instance) onHits(p *player) []gamedata.Inflict {
	var out []gamedata.Inflict
	for _, id := range slices.Sorted(maps.Keys(p.buffs)) {
		s, ok := i.data.Skill(id)
		if !ok {
			continue
		}
		points := p.buffs[id].points
		if st := s.Spec(points, p.buffs[id].trait); st.OnHit.Status != "" {
			out = append(out, st.OnHit.At(points))
		}
	}
	return out
}

// inflicting are ins as p puts them on a mob, damage over time's power from their attack of damage, with their statuses.
func (i *Instance) inflicting(p *player, damage gamedata.DamageType, ins []gamedata.Inflict) []model.Infliction {
	base := p.statuses.Attacking(model.PlayerAttacker(p.derived, damage))
	out := make([]model.Infliction, 0, len(ins))
	for _, in := range ins {
		if def, ok := i.data.Status(in.Status); ok {
			out = append(out, model.InflictionOf(def, in, model.EntityRef{Player: p.ID}, base, i.ticksFor))
		}
	}
	return out
}

// strike lands p's hit of damage through the arena, from where they stand or round their lock, their statuses' factors on its power: what it inflicts on each mob it lands on is own, then their buffs' on_hit. It credits what it does, and reports whether it reached a mob.
func (i *Instance) strike(p *player, s mobs.Strike, damage gamedata.DamageType, own gamedata.Inflict) bool {
	s.By, s.Aim, s.Power = p.ID, i.aimOf(p, s.Shape, s.Target), p.statuses.Attacking(s.Power)
	s.Inflicts = i.inflicting(p, damage, append([]gamedata.Inflict{own}, i.onHits(p)...))
	blows := i.arena.Strike(s)
	i.credit(p, blows)
	return len(blows) > 0
}

// afflict lands a status skill's stage, used with points, on every mob its shape reaches: no hit, so no buff's on_hit; its damage over time takes the class's damage type.
func (i *Instance) afflict(p *player, st gamedata.Stage, points int, target uint32) {
	i.arena.Afflict(mobs.Strike{
		By: p.ID, Shape: st.Shape, Most: math.MaxInt, Target: uint64(target), Aim: i.aimOf(p, st.Shape, uint64(target)), Push: st.Push,
		Inflicts: i.inflicting(p, p.class.Attack.Damage, []gamedata.Inflict{st.Status.At(points)}),
	})
}

// creditDots credits the mobs' damage over time this tick to their inflicters on the map, as their hits are.
func (i *Instance) creditDots() {
	for _, b := range i.arena.Dots() {
		if p, ok := i.players[b.Damage.Source.Player]; ok {
			i.credit(p, []mobs.Blow{b})
		}
	}
}

// restrain sets p's movement from their statuses for the input stepped this tick: held while one keeps them from moving, slowed while one slows them, each over its sub-steps.
func (i *Instance) restrain(p *player) {
	p.state.Held = int(p.statuses.Held(i.tick)) * i.subSteps //nolint:gosec // a status lasts seconds
	p.state.Slow = sim.Slow{}
	if left, factor := p.statuses.Slowed(i.tick); left > 0 {
		p.state.Slow = sim.Slow{Left: int(left) * i.subSteps, Factor: factor} //nolint:gosec // a status lasts seconds
	}
}

// inflict puts what a mob's attack inflicts on p from the next tick: a status that blocks acting ends the action under way, as a death does, with its move and dodge, and the attack held for it.
func (i *Instance) inflict(p *player, in model.Infliction) {
	if !p.statuses.Inflict(in, i.tick+1, 0, p.state.X, p.state.Z) {
		return
	}
	if m := p.moveMark; m != nil && m.breaks {
		return // a break-free pressed, still to break, ends this too
	}
	if in.Status.Blocks.Has(gamedata.BlocksAct) {
		p.action, p.windUps, p.attackQueued = action{}, nil, false
		p.moveMark, p.state.Move, p.dodgeUntil = nil, sim.Move{}, 0
	}
}

// runStatuses runs each living player's statuses for the tick: damage over time deals its steady damage, from the mob that put it on, a shield taking it first but the hit guard not stopping it; it wakes a sleeper and stops a teleport as a hit does, a death following. Then those run out end.
func (i *Instance) runStatuses() {
	for _, id := range i.order {
		p := i.players[id]
		if p.dead {
			continue
		}
		dots, ended := p.statuses.Tick(i.tick, p.state.X, p.state.Z)
		p.changed = p.changed || ended
		for _, d := range dots {
			amount := model.Steady(d.Power, p.statuses.Defending(model.PlayerDefender(p.derived)))
			past := p.shielded(amount)
			i.events = append(i.events, model.Damage{Tick: i.tick, Source: d.By, Target: model.EntityRef{Player: p.ID}, Amount: amount, Status: d.Status.ID, Shielded: amount - past})
			p.HP -= past
			p.changed = true
			if p.HP <= 0 {
				i.kill(p)
				break
			}
			p.statuses.Wake(i.tick)
			i.cancelTeleport(p, model.TeleportHit)
		}
	}
}
