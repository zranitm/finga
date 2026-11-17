package world

import (
	"math"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
)

// Statuses are a body's abnormal statuses, a mob's or a player's, and the tick from which a hard status takes again.
type Statuses struct {
	list   []Status
	immune uint64
}

// Status is one running status: until the tick it ends on, and for damage over time, its next tick and where the body stood at its last.
type Status struct {
	Def   gamedata.Status
	Until uint64
	By    EntityRef
	Power Attacker // damage over time: the inflicter's attack × the power, as it was inflicted
	Every uint64   // damage over time: ticks between its ticks
	Next  uint64
	x, z  float64
}

// Infliction is a status a hit lands with: how long it lasts and, for damage over time, how often it ticks and how hard.
type Infliction struct {
	Status gamedata.Status
	Ticks  uint64
	Every  uint64
	By     EntityRef
	Power  Attacker
}

// InflictionOf is in, a status of def by by, in a map's ticks: damage over time, alone, ticks every its every with base, the inflicter's attack, × its power. A knockdown's time is its down pose's, which the arena gives it.
func InflictionOf(def gamedata.Status, in gamedata.Inflict, by EntityRef, base Attacker, ticks func(time.Duration) uint64) Infliction {
	out := Infliction{Status: def, By: by}
	if in.Time > 0 {
		out.Ticks = ticks(in.Time)
	}
	if def.Group == gamedata.Dot {
		out.Every, out.Power = ticks(def.Every), base.Times(in.Power)
	}
	return out
}

// Dot is one tick of damage over time: its status, its inflicter, and its attack, a moving status's factor taken.
type Dot struct {
	Status gamedata.Status
	By     EntityRef
	Power  Attacker
}

// perInflicter is whether a status runs once per inflicter beside others' of its id: damage over time, and a mob losing sight of each who blinded it.
func perInflicter(s gamedata.Status) bool {
	return s.Group == gamedata.Dot || s.ID == gamedata.LoseSight
}

// Inflict starts in from tick from on a body standing at (x, z), in order: super armor (the share it leaves of a time, 0 for none) refuses a hard status and shortens another; hard immunity refuses a hard one; the same status restarts, its new inflicter and power taking over; or it starts. It reports whether it took.
func (s *Statuses) Inflict(in Infliction, from uint64, superArmor float64, x, z float64) bool {
	hard := in.Status.Group == gamedata.Hard
	ticks := in.Ticks
	switch {
	case hard && superArmor > 0:
		return false
	case superArmor > 0:
		ticks = max(1, uint64(math.Round(float64(ticks)*superArmor)))
	case hard && from < s.immune:
		return false
	}
	k := slices.IndexFunc(s.list, func(st Status) bool {
		return st.Def.ID == in.Status.ID && (!perInflicter(in.Status) || st.By == in.By)
	})
	if k >= 0 {
		st := &s.list[k] // a restart keeps damage over time's ticking on its beat, so hits faster than it never hold it off
		st.Until, st.By, st.Power = from+ticks, in.By, in.Power
		return true
	}
	s.list = append(s.list, Status{Def: in.Status, Until: from + ticks, By: in.By, Power: in.Power, Every: in.Every, Next: from + in.Every, x: x, z: z})
	return true
}

// Tick runs the statuses at tick for a body standing at (x, z): the damage over time due, each on its beat up to its last tick, then the end of those whose time is up, a hard one's starting immunity for immunity ticks. It reports what ended.
func (s *Statuses) Tick(tick, immunity uint64, x, z float64) (dots []Dot, ended bool) {
	for k := range s.list {
		st := &s.list[k]
		if st.Every == 0 || st.Next > tick || st.Next > st.Until {
			continue
		}
		power := st.Power
		if moved := x != st.x || z != st.z; moved && st.Def.Moving != 1 {
			power = power.Times(st.Def.Moving)
		}
		dots = append(dots, Dot{Status: st.Def, By: st.By, Power: power})
		st.Next += st.Every
		st.x, st.z = x, z
	}
	return dots, s.end(tick, immunity, func(st Status) bool { return st.Until <= tick })
}

// Shatter ends a status a hit that lands breaks, a freeze, and is that hit's factor, 1 for none.
func (s *Statuses) Shatter(tick, immunity uint64) float64 {
	f := 1.0
	for _, st := range s.list {
		if st.Def.Shatter > 0 {
			f *= st.Def.Shatter
		}
	}
	s.end(tick, immunity, func(st Status) bool { return st.Def.Shatter > 0 })
	return f
}

// Wake ends the statuses any damage ends, a sleep; it reports whether one did.
func (s *Statuses) Wake(tick, immunity uint64) bool {
	return s.end(tick, immunity, func(st Status) bool { return st.Def.Wakes })
}

// end drops the statuses gone says are over; a hard one's end starts immunity.
func (s *Statuses) end(tick, immunity uint64, gone func(Status) bool) bool {
	n := len(s.list)
	hard := false
	s.list = slices.DeleteFunc(s.list, func(st Status) bool {
		if !gone(st) {
			return false
		}
		hard = hard || st.Def.Group == gamedata.Hard
		return true
	})
	if hard {
		s.immune = max(s.immune, tick+immunity)
	}
	return len(s.list) < n
}

// DropBy ends the damage over time by, who left: its credit needs them.
func (s *Statuses) DropBy(by EntityRef) {
	s.list = slices.DeleteFunc(s.list, func(st Status) bool { return st.Def.Group == gamedata.Dot && st.By == by })
}

// Clear ends every status, and the immunity, as a death does.
func (s *Statuses) Clear() {
	s.list, s.immune = s.list[:0], 0
}

// Blocks is whether a running status blocks b.
func (s *Statuses) Blocks(b gamedata.Blocks) bool {
	return slices.ContainsFunc(s.list, func(st Status) bool { return st.Def.Blocks.Has(b) })
}

// Find is the running status of id, and for one per inflicter, by them; ok false for none.
func (s *Statuses) Find(id string, by EntityRef) (Status, bool) {
	k := slices.IndexFunc(s.list, func(st Status) bool {
		return st.Def.ID == id && (!perInflicter(st.Def) || st.By == by)
	})
	if k < 0 {
		return Status{}, false
	}
	return s.list[k], true
}

// Attacking is a, the body's attack, with its statuses' factors: weaken's attack and blind's accuracy.
func (s *Statuses) Attacking(a Attacker) Attacker {
	for _, st := range s.list {
		a.PhysicalAttack *= st.Def.Attack
		a.MagicAttack *= st.Def.Attack
		a.Accuracy *= st.Def.Accuracy
	}
	return a
}

// Defending is d, the body's defense, with its statuses' factors: armour break's.
func (s *Statuses) Defending(d Defender) Defender {
	for _, st := range s.list {
		d.PhysicalDefense *= st.Def.PhysicalDefense
		d.MagicDefense *= st.Def.MagicDefense
	}
	return d
}

// MoveSpeed is the factor the statuses put on the body's walking: slow's.
func (s *Statuses) MoveSpeed() float64 {
	f := 1.0
	for _, st := range s.list {
		f *= st.Def.MoveSpeed
	}
	return f
}
