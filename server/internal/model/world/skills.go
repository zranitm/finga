package world

import (
	"maps"
	"time"

	"finga/internal/shared/sim"
)

// SkillUse is a press of a skill: the skill, its key's root (the skill itself on its own key), the yaw it aims along, the locked mob (0 for none), and a moving skill's way.
type SkillUse struct {
	Skill, Root string
	Aim         float32
	Target      uint32
	Way         sim.Way
}

// Skills are a character's skill points: those invested in each skill, and those left to spend. Only its map's tick touches them; anything read off the tick, such as a checkpoint or a sheet, takes a Clone.
type Skills struct {
	Points  map[string]int // by skill; one with none isn't listed
	Unspent int
}

// Clone copies the points, so the copy shares nothing.
func (s Skills) Clone() Skills { return Skills{Points: maps.Clone(s.Points), Unspent: s.Unspent} }

// SkillTimers are when each skill's cooldown and buff end, by skill: wall-clock times, since each map counts its own ticks, so a map change, a checkpoint and a fresh join all carry them alike.
type SkillTimers struct {
	Cooldowns map[string]time.Time
	Buffs     map[string]BuffTimer
}

// BuffTimer is when a buff ends, the points it was used with, which give its bonus, and a shield's pool left; a map change carries the pool, and a checkpoint keeps none.
type BuffTimer struct {
	Until  time.Time
	Points int
	Shield int
}

// Clone copies the maps, so the copy shares nothing.
func (t SkillTimers) Clone() SkillTimers {
	return SkillTimers{Cooldowns: maps.Clone(t.Cooldowns), Buffs: maps.Clone(t.Buffs)}
}
