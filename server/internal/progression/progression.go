// Package progression holds Finga's stat and level rules as pure functions
// over values (page 5): derived stats from class formulas, primary stats,
// level and equipment; EXP awards with level-ups; the death penalty. The
// character service runs them when it creates or shows a character, and
// the world runs them inside a tick, so they live here, where both can
// import them, rather than inside either service.
package progression

// Progress is a level and the EXP within it.
type Progress struct {
	Level int
	Exp   int64
}

// Derived are the stats computed from class, primary stats, level and
// equipment (page 5). HP and MP are whole points; the rest stay fractional
// for the damage formula. Rates are fractions.
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

// Equipment is what worn items add. Stage 1 characters wear nothing, so
// callers pass the zero value; the starter items are inventory.
type Equipment struct {
	WeaponAttack float64
	ArmorDefense float64
	Penetration  float64
}

// ExpCurve is what the EXP rules need from game data.
type ExpCurve interface {
	ExpToNext(level int) (int64, bool)
	LevelCap() int
}
