// Package progression holds the stat and level rules shared by the character service and the world.
package progression

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

// Equipment is what worn items add.
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
