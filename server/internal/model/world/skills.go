package world

import (
	"maps"
	"time"

	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// SkillUse is a press of a skill: the skill, its key's root (the skill itself on its own key), the yaw it aims along, the locked mob (0 for none), and a moving skill's way.
type SkillUse struct {
	Skill, Root string
	Aim         float32
	Target      uint32
	Way         sim.Way
}

// Skills are a character's skill points: those invested in each skill, those left to spend, and each skill's chosen trait. Only its map's tick touches them; anything read off the tick, such as a checkpoint or a sheet, takes a Clone.
type Skills struct {
	Points  map[string]int    // by skill; one with none isn't listed
	Traits  map[string]string // by skill; one on its default isn't listed
	Unspent int
}

// Clone copies the points and traits, so the copy shares nothing.
func (s Skills) Clone() Skills {
	return Skills{Points: maps.Clone(s.Points), Traits: maps.Clone(s.Traits), Unspent: s.Unspent}
}

// SkillTimers are when each skill's cooldown and buff end, by skill: wall-clock times, since each map counts its own ticks, so a map change, a checkpoint and a fresh join all carry them alike.
type SkillTimers struct {
	Cooldowns map[string]time.Time
	Buffs     map[string]BuffTimer
}

// BuffTimer is when a buff ends, the points and trait it was used with, which give its bonus, a shield's pool left, and who used it; a map change carries the pool and the giver, and a checkpoint keeps neither.
type BuffTimer struct {
	Until  time.Time
	Points int
	Trait  string
	Shield int
	From   kernel.CharacterID
}

// Clone copies the maps, so the copy shares nothing.
func (t SkillTimers) Clone() SkillTimers {
	return SkillTimers{Cooldowns: maps.Clone(t.Cooldowns), Buffs: maps.Clone(t.Buffs)}
}
