package bots

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/internal/shared/steer"
)

// The ranges a bot keeps.
const (
	meleeShare = 0.7 // of its attack's reach, where a warrior or a thief stands
	rangedFar  = 9.0 // metres past which a bowman or a magician walks in, to rangedNear
	rangedNear = 6.0
	tooClose   = 3.0 // metres inside which a bowman or a magician steps away
)

// Setup is what a bot fights with: its seed, tier, class, build and skills, the map and its body.
type Setup struct {
	Seed   [2]uint64
	Tier   gamedata.BotTier
	Class  gamedata.Class
	Bar    []string
	Order  []string
	Skills map[string]Skill
	Map    *gamedata.Map
	Radius float64
	Height float64
	Tick   time.Duration
}

// Skill is a skill a bot has: its definition and the stage its points and trait make.
type Skill struct {
	gamedata.Skill
	Spec gamedata.Stage
}

// Brain picks a bot's keys and presses by a priority list, its chances rolled from its seed.
type Brain struct {
	s                    Setup
	rand                 *rand.Rand
	reaction, breakAfter uint64 // ticks
	breakFree, breakRoot string // its class's break-free and the root it's pressed from
	defences             []string
	walk                 steer.Walker
	stuck                steer.Stall
	held                 sim.Input
	hard                 bool   // a hard status held it at its last decision
	hardFrom             uint64 // since when
	breaking             bool   // it will break free this time
	defending            map[startedKey]bool
	following            map[[2]string]bool
	approaching          bool // a bowman or a magician walking in from past rangedFar
}

// startedKey names an action a fighter started.
type startedKey struct {
	by    kernel.CharacterID
	start uint64
}

// New is a bot's brain.
func New(s Setup) *Brain {
	b := &Brain{s: s, rand: rand.New(rand.NewPCG(s.Seed[0], s.Seed[1])), defending: map[startedKey]bool{}, following: map[[2]string]bool{}} //nolint:gosec // a bot's choices, not a secret
	b.reaction, b.breakAfter = model.TicksIn(s.Tier.Reaction, s.Tick), model.TicksIn(s.Tier.BreakFree, s.Tick)
	for _, ch := range s.Class.Chains {
		for _, f := range ch.Follow {
			if sk, ok := s.Skills[f.Skill]; ok && sk.BreaksControl && f.When.ControlledSelf && slices.Contains(s.Bar, ch.Root) {
				b.breakFree, b.breakRoot = f.Skill, ch.Root
			}
		}
	}
	for _, id := range s.Bar {
		if sk, ok := s.Skills[id]; ok && (sk.Spec.Dodge > 0 || sk.Spec.Block > 0) {
			b.defences = append(b.defences, id)
		}
	}
	return b
}

// Decide is what the bot does until its next decision.
func (b *Brain) Decide(v View) Decision {
	held := b.held
	d := b.decide(v, held)
	b.held = d.Keys
	return d
}

func (b *Brain) decide(v View, held sim.Input) Decision {
	me := v.Self
	if me.Down || !v.Fighting {
		return Decision{}
	}
	others, started := v.Ring.As(v.Tick-min(v.Tick, b.reaction), v.Tick)
	if me.Hard {
		return b.breakOut(v, others)
	}
	b.hard = false
	var d Decision
	d.Skill = b.defend(v, others, started)
	t := b.target(me, others)
	if t == nil {
		return d
	}
	at, to := point(me.State), nav.Point{X: t.Body.X, Z: t.Body.Z}
	dist := nav.Distance(at, to)
	lock := model.EntityRef{Player: t.ID}
	yaw := steer.YawTo(at, to) + float32((b.rand.Float64()*2-1)*b.s.Tier.Aim)
	way := sim.Way{X: math.Sin(float64(yaw)), Z: math.Cos(float64(yaw)), Length: dist}
	if d.Skill == nil {
		d.Skill = b.followUp(v, lock, yaw, way, dist)
	}
	if d.Skill == nil && me.SkillFrom <= v.Tick {
		for _, id := range b.s.Order {
			if b.ready(me, id, v.Tick, dist) {
				d.Skill = &model.SkillUse{Skill: id, Root: id, Aim: yaw, Target: lock, Way: way}
				break
			}
		}
	}
	if dist <= reachOf(b.s.Class.Attack.Shape) && !me.Attacking && !me.CantAct {
		d.Attack = &Attack{Aim: yaw, Lock: lock}
	}
	d.Keys = b.keep(v, held, at, to, dist)
	return d
}

// breakOut is a held bot's: at its tier's defend chance, rolled once for each hold, it presses its break-free from its root once the hold has lasted its tier's break_free.
func (b *Brain) breakOut(v View, others []Seen) Decision {
	if !b.hard {
		b.hard, b.hardFrom, b.breaking = true, v.Tick, b.roll(b.s.Tier.Defend)
	}
	if !b.breaking || b.breakFree == "" || v.Tick < b.hardFrom+b.breakAfter || !b.usable(v.Self, b.breakFree, v.Tick) {
		return Decision{}
	}
	b.breaking = false
	at := point(v.Self.State)
	use := &model.SkillUse{Skill: b.breakFree, Root: b.breakRoot, Aim: model.YawOf(v.Self.State.HX, v.Self.State.HZ), Way: sim.Way{X: v.Self.State.HX, Z: v.Self.State.HZ}}
	if t := b.target(v.Self, others); t != nil {
		use.Aim, use.Target, use.Way = steer.YawTo(at, nav.Point{X: t.Body.X, Z: t.Body.Z}), model.EntityRef{Player: t.ID}, away(at, t.Body)
	}
	if mv := b.s.Skills[b.breakFree].Spec.Move; mv.Distance > 0 {
		use.Way.Length = mv.Distance
	}
	return Decision{Skill: use}
}

// defend guards a delayed hit that will reach the bot, or the next blow of an enemy in reach, once for each action at its tier's chance.
func (b *Brain) defend(v View, others []Seen, started []Started) *model.SkillUse {
	for k := range b.defending {
		if !slices.ContainsFunc(started, func(s Started) bool { return s.By == k.by && s.Start == k.start }) {
			delete(b.defending, k) // over: the ring shows an action until its hit and next blow have passed
		}
	}
	me := v.Self
	at := point(me.State)
	for _, s := range started {
		by := find(others, s.By)
		if by == nil || by.Side == me.Side || !by.Up || by.Hard {
			continue // down or held, its action is over
		}
		var guard []uint64
		if s.Blow && s.Hit >= v.Tick && b.reaches(v, s, others) {
			guard = append(guard, s.Hit)
		}
		if s.Next >= v.Tick && nav.Distance(b.ahead(v, s.Next), nav.Point{X: by.Body.X, Z: by.Body.Z}) <= reachOf(by.Attack)+b.s.Radius {
			guard = append(guard, s.Next)
		}
		if len(guard) == 0 {
			continue
		}
		k := startedKey{s.By, s.Start}
		want, rolled := b.defending[k]
		if !rolled {
			want = b.roll(b.s.Tier.Defend)
			b.defending[k] = want
		}
		if !want {
			continue
		}
		for _, t := range guard {
			for _, id := range b.defences {
				st := b.s.Skills[id].Spec
				start := max(v.Tick, me.SkillFrom, me.Ready[id])
				runs := model.TicksIn(max(st.Dodge, st.Block), b.s.Tick)
				if start > t || t >= start+runs || !b.usable(me, id, start) {
					continue
				}
				use := &model.SkillUse{Skill: id, Root: id, Aim: steer.YawTo(at, nav.Point{X: by.Body.X, Z: by.Body.Z}), Target: model.EntityRef{Player: by.ID}, Way: away(at, by.Body)}
				use.Way.Length = st.Move.Distance
				b.defending[k] = false // one guard for each action
				return use
			}
		}
	}
	return nil
}

// ahead is where the bot will stand at tick, going as it goes now.
func (b *Brain) ahead(v View, tick uint64) nav.Point {
	me := v.Self.State
	secs := float64(tick-min(tick, v.Tick)) * b.s.Tick.Seconds()
	return nav.Point{X: me.X + float64(me.VX*secs), Z: me.Z + float64(me.VZ*secs)}
}

// reaches is whether s's blow takes in the bot where it will stand at the hit.
func (b *Brain) reaches(v View, s Started, others []Seen) bool {
	at := b.ahead(v, s.Hit)
	body := model.Body{X: at.X, Y: v.Self.State.Y, Z: at.Z, Radius: b.s.Radius, Height: b.s.Height}
	lock, found := gamedata.Point{}, false
	switch {
	case s.Lock.Player == v.Self.ID:
		lock, found = gamedata.Point{X: body.X, Z: body.Z}, true
	case s.Lock.Player != kernel.CharacterID{}:
		if l := find(others, s.Lock.Player); l != nil {
			lock, found = gamedata.Point{X: l.Body.X, Z: l.Body.Z}, true
		}
	}
	aim := s.Aim
	if dx, dz := lock.X-aim.X, lock.Z-aim.Z; found && s.Toward > 0 {
		if d := nav.Length(dx, dz); d > 1e-9 {
			step := min(s.Toward, max(0, d-2*b.s.Radius))
			aim.X, aim.Z = aim.X+dx/d*step, aim.Z+dz/d*step
		}
	}
	if s.Shape.At == gamedata.AtLock {
		aim = aim.AtLock(lock, found, s.Shape.Reach)
	}
	return len(model.Reached(aim, s.Shape, []model.Body{body}, 1)) > 0
}

// followUp takes a follow-up its keys offer, at its tier's chain chance, rolled once each time one is offered: the first of its chains' that holds, reaching its target.
func (b *Brain) followUp(v View, lock model.EntityRef, yaw float32, way sim.Way, dist float64) *model.SkillUse {
	if v.Holds == nil {
		return nil
	}
	var use *model.SkillUse
	for _, ch := range b.s.Class.Chains {
		if !slices.Contains(b.s.Bar, ch.Root) {
			continue
		}
		for _, f := range ch.Follow {
			k := [2]string{ch.Root, f.Skill}
			sk, ok := b.s.Skills[f.Skill]
			if !ok || sk.BreaksControl || !v.Holds(ch.Root, f.Skill, lock) {
				delete(b.following, k)
				continue
			}
			want, rolled := b.following[k]
			if !rolled {
				want = b.roll(b.s.Tier.Chain)
				b.following[k] = want
			}
			if want && use == nil && v.Self.SkillFrom <= v.Tick && b.ready(v.Self, f.Skill, v.Tick, dist) {
				use = &model.SkillUse{Skill: f.Skill, Root: ch.Root, Aim: yaw, Target: lock, Way: way}
			}
		}
	}
	return use
}

// keep is the keys that hold the bot's range from its target.
func (b *Brain) keep(v View, held sim.Input, at, to nav.Point, dist float64) sim.Input {
	stalled := b.stuck.Stalled(at, v.Self.State.OnGround, held)
	now := time.Duration(v.Tick) * b.s.Tick
	if b.s.Class.Attack.Shape.Kind == gamedata.ArcShape {
		if dist > reachOf(b.s.Class.Attack.Shape)*meleeShare {
			return b.walk.Keys(b.s.Map, b.s.Radius, at, to, now, stalled)
		}
		return sim.Input{}
	}
	switch {
	case dist > rangedFar:
		b.approaching = true
	case dist <= rangedNear:
		b.approaching = false
	}
	switch {
	case b.approaching:
		return b.walk.Keys(b.s.Map, b.s.Radius, at, to, now, stalled)
	case dist < tooClose:
		return steer.KeysToward(at.X-to.X, at.Z-to.Z)
	}
	return sim.Input{}
}

// target is the enemy the bot fights: the nearest still up.
func (b *Brain) target(me Self, others []Seen) *Seen {
	var best *Seen
	bestD := math.Inf(1)
	for k := range others {
		o := &others[k]
		if o.ID == me.ID || o.Side == me.Side || !o.Up {
			continue
		}
		if d := nav.Distance(point(me.State), nav.Point{X: o.Body.X, Z: o.Body.Z}); d < bestD {
			best, bestD = o, d
		}
	}
	return best
}

// ready is whether the bot would use skill now at dist metres from its target: usable, and a damage or status skill's shape reaching, past a move to the lock if it has one, or a buff not running.
func (b *Brain) ready(me Self, id string, tick uint64, dist float64) bool {
	sk, ok := b.s.Skills[id]
	if !ok || !b.usable(me, id, tick) {
		return false
	}
	switch sk.Effect {
	case gamedata.BuffEffect:
		return !slices.Contains(me.Buffs, id)
	case gamedata.DamageEffect, gamedata.StatusEffect:
		reach := reachOf(sk.Spec.Shape)
		if mv := sk.Spec.Move; mv.Toward == gamedata.TowardLock {
			reach += mv.Distance
		}
		return dist <= reach
	}
	return false
}

// usable is whether the world would take the skill's press for a start at tick: one it has, no use waiting, free to act and not silenced, cooled down, and paid for.
func (b *Brain) usable(me Self, id string, tick uint64) bool {
	sk, ok := b.s.Skills[id]
	return ok && !me.Waiting && (sk.BreaksControl || !me.CantAct && !me.Silenced) && me.Ready[id] <= tick && me.Energy >= sk.Spec.Energy
}

// roll is true at chance.
func (b *Brain) roll(chance float64) bool { return b.rand.Float64() < chance }

// reachOf is how far from its user a shape reaches.
func reachOf(s gamedata.Shape) float64 {
	switch {
	case s.Kind == gamedata.ShotShape:
		return s.Range
	case s.At == gamedata.AtLock:
		return s.Reach + s.Radius
	}
	return s.Radius
}

// away is the unit way from a body to the bot at at, straight back from it.
func away(at nav.Point, from model.Body) sim.Way {
	dx, dz := at.X-from.X, at.Z-from.Z
	if l := nav.Length(dx, dz); l > 1e-9 {
		return sim.Way{X: dx / l, Z: dz / l}
	}
	return sim.Way{Z: -1}
}

func find(seen []Seen, id kernel.CharacterID) *Seen {
	for k := range seen {
		if seen[k].ID == id {
			return &seen[k]
		}
	}
	return nil
}

func point(s sim.State) nav.Point { return nav.Point{X: s.X, Z: s.Z} }
