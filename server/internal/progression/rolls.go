package progression

import "finga/internal/gamedata"

// Rolled is a roll that picks each stat's value from its range, every stat in one fixed order, so a seeded pick rolls the same.
func Rolled(r gamedata.Rolls, pick func(gamedata.Range) int) ItemStats {
	var s ItemStats
	for _, f := range fieldsOf(r, &s) {
		*f.value = pick(f.in)
	}
	return s
}

// Lowest is the roll at the bottom of every range, which starter equipment gets.
func Lowest(r gamedata.Rolls) ItemStats {
	return Rolled(r, func(in gamedata.Range) int { return in.Min })
}

// Fits reports whether every stat lies in its range, ends included; a stat the item doesn't give must be 0.
func (s ItemStats) Fits(r gamedata.Rolls) bool {
	for _, f := range fieldsOf(r, &s) {
		if *f.value < f.in.Min || *f.value > f.in.Max {
			return false
		}
	}
	return true
}

type field[T any] struct {
	in    T
	value *int
}

// fieldsOf pairs each stat of r with its value in s.
func fieldsOf[T any](r gamedata.StatSet[T], s *ItemStats) [21]field[T] {
	ins := [21]T{
		r.STR, r.DEX, r.INT, r.AGI, r.VIT,
		r.PhysicalAttack, r.MagicAttack, r.PhysicalDefense, r.MagicDefense,
		r.MaxHP, r.MaxMP, r.Accuracy, r.Evasion,
		r.CritRate, r.CritDamage, r.Penetration, r.HPRegen, r.MPRegen,
		r.AttackSpeed, r.CooldownSpeed, r.MoveSpeed,
	}
	var out [21]field[T]
	for k, v := range s.values() {
		out[k] = field[T]{ins[k], v}
	}
	return out
}
