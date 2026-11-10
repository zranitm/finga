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

type field struct {
	in    gamedata.Range
	value *int
}

// fieldsOf pairs each stat's range with its value in s.
func fieldsOf(r gamedata.Rolls, s *ItemStats) [21]field {
	ranges := [21]gamedata.Range{
		r.STR, r.DEX, r.INT, r.AGI, r.VIT,
		r.PhysicalAttack, r.MagicAttack, r.PhysicalDefense, r.MagicDefense,
		r.MaxHP, r.MaxMP, r.Accuracy, r.Evasion,
		r.CritRate, r.CritDamage, r.Penetration, r.HPRegen, r.MPRegen,
		r.AttackSpeed, r.CooldownSpeed, r.MoveSpeed,
	}
	var out [21]field
	for k, v := range s.values() {
		out[k] = field{ranges[k], v}
	}
	return out
}
