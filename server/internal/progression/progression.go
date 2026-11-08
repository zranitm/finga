// Package progression holds the stat and level rules shared by the character service and the world.
package progression

import "finga/internal/gamedata"

// Progress is a level and the EXP within it.
type Progress struct {
	Level int
	Exp   int64
}

// Derived are the computed stats; HP and MP are whole points, rates are fractions.
type Derived struct {
	MaxHP       int
	MaxMP       int
	AttackPower float64
	Accuracy    float64
	Evasion     float64
	Defense     float64
	CritRate    float64
	CritDamage  float64
	Penetration float64
}

// ItemStats are what equipment gives, a whole number per stat, the rates in tenths of a percent: one copy's roll, or all a character wears.
type ItemStats struct {
	Attack, Defense, STR, DEX, INT, AGI, VIT, Accuracy, Evasion, CritRate, CritDamage, MaxHP, MaxMP int
}

// Primary are the primary stats among them.
func (s ItemStats) Primary() gamedata.Stats {
	return gamedata.Stats{STR: s.STR, DEX: s.DEX, INT: s.INT, AGI: s.AGI, VIT: s.VIT}
}

// ExpCurve is what the EXP rules need from game data.
type ExpCurve interface {
	ExpToNext(level int) (int64, bool)
	LevelCap() int
}
