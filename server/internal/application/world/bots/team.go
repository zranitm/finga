package bots

import (
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
)

// A side's play in a team mode.
const (
	focusReach = 12.0            // metres from one of its side within which an enemy can be its focus
	focusHold  = 2 * time.Second // how long a side keeps its focus before it may change
	healBelow  = 0.6             // the HP share under which one of the side is healed
)

// Team is what a side's bots share in a team mode: the enemy they focus, and until when they hold it. Its zero value has none.
type Team struct {
	focus kernel.CharacterID
	until uint64 // the tick the hold ends
}

// focusOf is the side's focus as me sees it at tick: the one held while it's up and the hold lasts, else the enemy up with the lowest HP share within focusReach of one of the side, held from now; nil when no enemy is that near.
func (t *Team) focusOf(tick, hold uint64, me Self, seen []Seen) *Seen {
	if f := find(seen, t.focus); f != nil && f.Up && tick < t.until {
		return f
	}
	t.focus = kernel.CharacterID{}
	var best *Seen
	for k := range seen {
		o := &seen[k]
		if o.Side != me.Side && o.Up && nearSide(o.Body, me, seen) && (best == nil || o.HP*best.MaxHP < best.HP*o.MaxHP) {
			best = o
		}
	}
	if best != nil {
		t.focus, t.until = best.ID, tick+hold
	}
	return best
}

// nearSide is whether a body stands within focusReach of me now, or of one of me's side up as seen.
func nearSide(b model.Body, me Self, seen []Seen) bool {
	at := nav.Point{X: b.X, Z: b.Z}
	if nav.Distance(at, point(me.State)) <= focusReach {
		return true
	}
	for _, s := range seen {
		if s.Side == me.Side && s.ID != me.ID && s.Up && nav.Distance(at, nav.Point{X: s.Body.X, Z: s.Body.Z}) <= focusReach {
			return true
		}
	}
	return false
}

// mend is the bot's heal for the lowest of its side under healBelow of their HP, itself among them, when one is ready: pressed once it would reach them, else the place to walk to first.
func (b *Brain) mend(v View, others []Seen, yaw float32, lock model.EntityRef) (*model.SkillUse, *nav.Point) {
	me := v.Self
	id := ""
	for _, h := range b.healing {
		if b.usable(me, h, v.Tick) {
			id = h
			break
		}
	}
	if id == "" {
		return nil, nil
	}
	low := func(hp, most int) bool { return float64(hp) < healBelow*float64(most) }
	var lowest *Seen // nil while the bot itself is the lowest
	hp, most, found := me.HP, me.MaxHP, low(me.HP, me.MaxHP)
	for k := range others {
		o := &others[k]
		if o.Side == me.Side && o.ID != me.ID && o.Up && low(o.HP, o.MaxHP) && (!found || o.HP*most < hp*o.MaxHP) {
			lowest, hp, most, found = o, o.HP, o.MaxHP, true
		}
	}
	if !found {
		return nil, nil
	}
	if lowest == nil || b.heals(me, others, b.s.Skills[id].Spec, lowest) {
		return &model.SkillUse{Skill: id, Root: id, Aim: yaw, Target: lock}, nil
	}
	return nil, &nav.Point{X: lowest.Body.X, Z: lowest.Body.Z}
}

// heals is whether a heal of st pressed now would take t, as the world deals it: the caster if hurt, then the nearest hurt of its side within the radius, up to its targets.
func (b *Brain) heals(me Self, others []Seen, st gamedata.Stage, t *Seen) bool {
	middle := me.State.Y + b.s.Height/2
	gap := func(o *Seen) float64 { return model.Gap(me.State.X, middle, me.State.Z, o.Body) }
	if gap(t) > st.Shape.Radius {
		return false
	}
	left := st.Targets
	if me.HP < me.MaxHP {
		left--
	}
	for k := range others {
		if o := &others[k]; o.ID != t.ID && o.ID != me.ID && o.Side == me.Side && o.Up && o.HP < o.MaxHP && gap(o) < gap(t) {
			left--
		}
	}
	return left > 0
}
