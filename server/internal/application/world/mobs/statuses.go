package mobs

import (
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// pushTime is how long a skill's push takes to move a mob its distance.
const pushTime = 200 * time.Millisecond

// pushing is a push under way: left steps of step metres along (ux, uz).
type pushing struct {
	ux, uz, step float64
	left         uint64
}

// Dots are this tick's damage over time on the mobs, each credited to its inflicter, those of a mob it killed with the kill.
func (a *Arena) Dots() []Blow { return a.dots }

// DropBy ends the damage over time id put on the mobs, who left the map: its credit needs them there.
func (a *Arena) DropBy(id kernel.CharacterID) {
	for _, mb := range a.mobs {
		mb.statuses.DropBy(model.EntityRef{Player: id})
	}
}

// runStatuses runs each mob's statuses for the tick, before any turn: damage over time deals its steady damage and wakes a sleeper, a kill taking the mob off; then those run out end.
func (a *Arena) runStatuses() {
	var dead []*mob
	for _, mb := range a.mobs {
		dots, ended := mb.statuses.Tick(a.tick, mb.x, mb.z)
		mb.changed = mb.changed || ended
		for _, d := range dots {
			if mb.hp <= 0 {
				break
			}
			damage := model.Steady(d.Power, mb.statuses.Defending(model.MobDefender(mb.def)))
			b := Blow{Damage: model.Damage{Tick: a.tick, Source: d.By, Target: model.EntityRef{Mob: mb.id}, Amount: damage, Status: d.Status.ID}, Dealt: min(damage, mb.hp)}
			mb.hp -= damage
			mb.changed = true
			mb.statuses.Wake(a.tick)
			if mb.hp <= 0 {
				b.Killed = &Kill{Mob: mb.def, X: mb.x, Y: mb.y, Z: mb.z}
				dead = append(dead, mb)
			}
			a.dots = append(a.dots, b)
		}
	}
	for _, mb := range dead {
		a.remove(mb)
	}
}

// inflict puts in on mb from the next tick, as a blow's effects count, and reports whether it took: a knockdown lasts the down pose, a hard status ends an attack under way, a taunt makes its inflicter the mob's provoker and target, and losing sight drops them.
func (a *Arena) inflict(mb *mob, in model.Infliction) bool {
	if in.Status.ID == gamedata.Knockdown {
		in.Ticks = a.ticks(a.data.MobRules().React.Down)
	}
	armor := 0.0
	if mb.def.SuperArmor {
		armor = a.data.StatusRules().SuperArmor
	}
	if !mb.statuses.Inflict(in, a.tick+1, armor, mb.x, mb.z) {
		return false
	}
	mb.changed = true
	st, _ := mb.statuses.Find(in.Status.ID, in.By)
	if in.Status.Blocks.Has(gamedata.BlocksAct) {
		mb.attack, mb.phase = -1, idle // a spit already thrown flies on
	}
	if in.Status.Blocks.Has(gamedata.BlocksMove) {
		mb.path, mb.pathFor = nil, kernel.CharacterID{} // a push under way still moves it
	}
	by := in.By.Player
	switch in.Status.ID {
	case gamedata.Knockdown:
		mb.pose, mb.poseEnd = model.Down, st.Until
	case gamedata.Taunt:
		mb.provoker, mb.provokedUntil = by, st.Until
		a.take(mb, by)
	case gamedata.LoseSight:
		if mb.target == by {
			mb.target, mb.path, mb.pathFor = kernel.CharacterID{}, nil, kernel.CharacterID{}
		}
		if mb.provoker == by {
			mb.provokedUntil = 0
		}
	}
	return true
}

// knockdown is the down pose as a status, by the player whose blow brought it.
func (a *Arena) knockdown(by kernel.CharacterID) model.Infliction {
	def, _ := a.data.Status(gamedata.Knockdown) // the loader holds statuses.yaml to one
	return model.Infliction{Status: def, By: model.EntityRef{Player: by}}
}

// lost is whether mb can't see player id: they put lose_sight on it.
func (mb *mob) lost(id kernel.CharacterID) bool {
	_, ok := mb.statuses.Find(gamedata.LoseSight, model.EntityRef{Player: id})
	return ok
}

// provokable is whether a blow from id makes them mb's provoker: not while it can't see them, nor while another's taunt holds it.
func (mb *mob) provokable(id kernel.CharacterID) bool {
	t, taunted := mb.statuses.Find(gamedata.Taunt, model.EntityRef{})
	return !mb.lost(id) && (!taunted || t.By.Player == id)
}

// held is whether a status keeps mb from walking.
func (mb *mob) held() bool { return mb.statuses.Blocks(gamedata.BlocksMove) }

// shove starts pushing mb d metres away from the aim's origin over pushTime, stopped by solids, then a flinch; it ends a windup or a leap or rush under way.
func (a *Arena) shove(mb *mob, from model.Aim, d float64) {
	ux, uz := away(from.X, from.Z, mb.x, mb.z, float32(from.Yaw))
	n := a.ticks(pushTime)
	mb.push = pushing{ux: ux, uz: uz, step: d / float64(n), left: n}
	if mb.phase == windingUp || mb.phase == travelling {
		mb.attack, mb.phase = -1, idle
	}
	mb.pose, mb.poseEnd = model.Flinching, a.tick+1+n+a.ticks(a.data.MobRules().React.Flinch)
	mb.changed = true
}

// pushed moves a mob under a push a step, which is its turn; a solid or a player that stops it ends the push.
func (a *Arena) pushed(mb *mob) bool {
	p := &mb.push
	if p.left == 0 {
		return false
	}
	p.left--
	if a.move(mb, p.ux*p.step, p.uz*p.step, &a.blocking) < p.step-1e-9 {
		p.left = 0
	}
	return true
}

// Afflict puts a status skill on each mob its shape reaches, up to Most, its target first: no damage and no roll, so it always lands, provoking as a blow does unless it makes the mob lose sight of its user; a push moves a mob without super armor or the down pose.
func (a *Arena) Afflict(s Strike) {
	hides := slices.ContainsFunc(s.Inflicts, func(in model.Infliction) bool { return in.Status.ID == gamedata.LoseSight })
	for _, mb := range a.reached(s) {
		if !hides && mb.provokable(s.By) {
			mb.provoker, mb.provokedUntil = s.By, a.afterBlow(a.data.MobRules().Provoke)
		}
		if s.Push > 0 && !mb.def.SuperArmor && mb.pose != model.Down && mb.pose != model.Rising {
			a.shove(mb, s.Aim, s.Push)
		}
		for _, in := range s.Inflicts {
			a.inflict(mb, in)
		}
	}
}

// landing is attack k of mb landing on hits: its power with mb's statuses, and the status it inflicts, if any.
func (a *Arena) landing(mb *mob, k int, hits []Hit) Landing {
	at := mb.def.Attacks[k]
	l := Landing{Mob: mb.id, Attack: k, Hits: hits, Unblockable: at.Unblockable, power: mb.statuses.Attacking(model.MobAttacker(mb.def, at.Power))}
	if in := at.Status; in.Status != "" {
		def, _ := a.data.Status(in.Status) // the loader holds an attack's status to statuses.yaml
		l.Inflicts = []model.Infliction{model.InflictionOf(def, in, model.EntityRef{Mob: mb.id}, mb.statuses.Attacking(model.MobAttacker(mb.def, 1)), a.ticks)}
		if in.Status == gamedata.Knockdown {
			l.Inflicts[0].Ticks = a.ticks(a.data.MobRules().React.Down)
		}
	}
	return l
}

// shattered is a landed hit's damage on mb after a status it breaks, a freeze: rounded, its factor taken.
func (a *Arena) shattered(mb *mob, damage int) int {
	if f := mb.statuses.Shatter(a.tick); f != 1 {
		return int(math.Round(float64(damage) * f))
	}
	return damage
}
