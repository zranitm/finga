package gamedata

import "finga/internal/kernel"

// Stats are the five primary stats.
type Stats struct {
	STR int `json:"str"`
	DEX int `json:"dex"`
	INT int `json:"int"`
	AGI int `json:"agi"`
	VIT int `json:"vit"`
}

// Total is the sum of the five stats.
func (s Stats) Total() int { return s.STR + s.DEX + s.INT + s.AGI + s.VIT }

// StatSet holds one value for each stat equipment and skills can give, the five primary and the sixteen derived; fields, not a map, so every sum runs in one order.
type StatSet[T any] struct {
	STR, DEX, INT, AGI, VIT                                    T
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense T
	MaxHP, MaxMP, Accuracy, Evasion                            T
	CritRate, CritDamage, Penetration, HPRegen, MPRegen        T
	AttackSpeed, CooldownSpeed, MoveSpeed                      T
}

// Primary names one of the five primary stats.
type Primary int

// The primary stats, in their fixed order.
const (
	STR Primary = iota
	DEX
	INT
	AGI
	VIT
)

// Of is one primary stat's value.
func (s Stats) Of(p Primary) int {
	return [...]int{s.STR, s.DEX, s.INT, s.AGI, s.VIT}[p]
}

// PerPrimary holds one value per primary stat.
type PerPrimary[T any] struct {
	STR, DEX, INT, AGI, VIT T
}

// Flats holds one value per flat derived stat, the ones a point adds a fixed amount to; fields, not a map, so every sum runs in one order.
type Flats[T any] struct {
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense    T
	MaxHP, MaxMP, Accuracy, Evasion, CritDamage, HPRegen, MPRegen T
}

// Rates holds one value per rate with diminishing returns.
type Rates[T any] struct {
	CritRate, AttackSpeed, CooldownSpeed, MoveSpeed T
}

// Grows is a class's base for a flat stat at level 1, and what each level after adds.
type Grows struct {
	Base, PerLevel float64
}

// At is the base at level.
func (g Grows) At(level int) float64 { return g.Base + g.PerLevel*float64(level-1) }

// Curve is a rate with diminishing returns over one primary stat: base + (limit − base) × points ÷ (points + k).
type Curve struct {
	Stat           Primary
	Base, Limit, K float64
}

// StatRules are data/stats.yaml: what each point gives, every class alike, each class's bases, and the limits.
type StatRules struct {
	PointsPerLevel      int
	SkillPointsPerLevel int // given each level, beside the stat points
	PerPoint            PerPrimary[Flats[float64]]
	Curves              Rates[Curve]
	Penetration         float64 // its limit; only gear gives it
	MinHit, MaxHit      float64 // what the hit chance is held between
	Classes             map[kernel.Class]Flats[Grows]
}

// StatRules are what each point gives, each class's bases, and the limits.
func (d *Data) StatRules() StatRules { return d.p.StatRules }
