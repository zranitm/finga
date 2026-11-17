package world

import (
	"math"
	"slices"
	"strings"
	"time"

	"finga/internal/shared/gamedata"
)

// Statuses are a body's abnormal statuses, a mob's or a player's, and the tick each status last ended on.
type Statuses struct {
	list  []Status
	ended map[string]uint64
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

// Inflict starts in from tick from on a body standing at (x, z), in order: super armor (the share it leaves of a time, 0 for none) refuses a hard status and shortens another; the same status restarts, its new inflicter and power taking over; or it starts. It reports whether it took.
func (s *Statuses) Inflict(in Infliction, from uint64, superArmor float64, x, z float64) bool {
	hard := in.Status.Group == gamedata.Hard
	ticks := in.Ticks
	switch {
	case hard && superArmor > 0:
		return false
	case superArmor > 0:
		ticks = max(1, uint64(math.Round(float64(ticks)*superArmor)))
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

// Tick runs the statuses at tick for a body standing at (x, z): the damage over time due, each on its beat up to its last tick, then the end of those whose time is up. It reports what ended.
func (s *Statuses) Tick(tick uint64, x, z float64) (dots []Dot, ended bool) {
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
	return dots, s.end(tick, func(st Status) bool { return st.Until <= tick })
}

// Shatter ends a status a hit that lands breaks, a freeze, and is that hit's factor, 1 for none.
func (s *Statuses) Shatter(tick uint64) float64 {
	f := 1.0
	for _, st := range s.list {
		if st.Def.Shatter > 0 {
			f *= st.Def.Shatter
		}
	}
	s.end(tick, func(st Status) bool { return st.Def.Shatter > 0 })
	return f
}

// Wake ends the statuses any damage ends, a sleep; it reports whether one did.
func (s *Statuses) Wake(tick uint64) bool {
	return s.end(tick, func(st Status) bool { return st.Def.Wakes })
}

// end drops the statuses gone says are over, noting the tick each ended on.
func (s *Statuses) end(tick uint64, gone func(Status) bool) bool {
	n := len(s.list)
	s.list = slices.DeleteFunc(s.list, func(st Status) bool {
		if !gone(st) {
			return false
		}
		if s.ended == nil {
			s.ended = map[string]uint64{}
		}
		s.ended[st.Def.ID] = tick
		return true
	})
	return len(s.list) < n
}

// DropBy ends the damage over time by, who left: its credit needs them.
func (s *Statuses) DropBy(by EntityRef) {
	s.list = slices.DeleteFunc(s.list, func(st Status) bool { return st.Def.Group == gamedata.Dot && st.By == by })
}

// Clear ends every status, as a death does.
func (s *Statuses) Clear() {
	s.list = s.list[:0]
	clear(s.ended)
}

// Holds is whether one of ids runs at tick, or ended within slack ticks of it.
func (s *Statuses) Holds(ids []string, tick, slack uint64) bool {
	for _, id := range ids {
		if slices.ContainsFunc(s.list, func(st Status) bool { return st.Def.ID == id }) {
			return true
		}
		if end, ok := s.ended[id]; ok && tick <= end+slack {
			return true
		}
	}
	return false
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

// Held is how many ticks from tick on a status keeps the body from moving; 0 when none does.
func (s *Statuses) Held(tick uint64) uint64 {
	var left uint64
	for _, st := range s.list {
		if st.Def.Blocks.Has(gamedata.BlocksMove) && st.Until > tick {
			left = max(left, st.Until-tick)
		}
	}
	return left
}

// Slowed is how many ticks from tick its walk stays at the factor of the move speeds of its statuses still running, to the first of them to end; 0 when none slows it.
func (s *Statuses) Slowed(tick uint64) (left uint64, factor float64) {
	factor = 1
	for _, st := range s.list {
		if st.Def.MoveSpeed == 1 || st.Until <= tick {
			continue
		}
		factor *= st.Def.MoveSpeed
		if left == 0 || st.Until-tick < left {
			left = st.Until - tick
		}
	}
	return left, factor
}

// StatusView is a status as players see it: its id and the tick it ends on, the latest of its kind's.
type StatusView struct {
	ID   string
	Ends uint64
}

// Shown is the body's statuses as players see them, one per kind, by id.
func (s *Statuses) Shown() []StatusView {
	var out []StatusView
	for _, st := range s.list {
		if k := slices.IndexFunc(out, func(v StatusView) bool { return v.ID == st.Def.ID }); k >= 0 {
			out[k].Ends = max(out[k].Ends, st.Until)
		} else {
			out = append(out, StatusView{ID: st.Def.ID, Ends: st.Until})
		}
	}
	slices.SortFunc(out, func(a, b StatusView) int { return strings.Compare(a.ID, b.ID) })
	return out
}
