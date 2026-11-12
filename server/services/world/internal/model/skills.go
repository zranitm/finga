package model

import (
	"maps"
	"time"

	"finga/internal/progression"
)

// Skills are a character's skill points: those invested in each skill, and those left to spend. Only its map's tick touches them; anything read off the tick, such as a checkpoint or a sheet, takes a Clone.
type Skills struct {
	Points  map[string]int // by skill; one with none isn't listed
	Unspent int
}

// Clone copies the points, so the copy shares nothing.
func (s Skills) Clone() Skills { return Skills{Points: maps.Clone(s.Points), Unspent: s.Unspent} }

// SkillTimers are the time left on each skill's cooldown and buff, by skill: a map change carries them so, since each map counts its own ticks.
type SkillTimers struct {
	Cooldowns map[string]time.Duration
	Buffs     map[string]BuffLeft
}

// BuffLeft is a buff's bonus and the time it has left.
type BuffLeft struct {
	Left  time.Duration
	Bonus progression.ItemStats
}
