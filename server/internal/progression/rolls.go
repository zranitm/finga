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
func fieldsOf(r gamedata.Rolls, s *ItemStats) [13]field {
	return [13]field{
		{r.Attack, &s.Attack}, {r.Defense, &s.Defense},
		{r.STR, &s.STR}, {r.DEX, &s.DEX}, {r.INT, &s.INT}, {r.AGI, &s.AGI}, {r.VIT, &s.VIT},
		{r.Accuracy, &s.Accuracy}, {r.Evasion, &s.Evasion}, {r.CritRate, &s.CritRate}, {r.CritDamage, &s.CritDamage},
		{r.MaxHP, &s.MaxHP}, {r.MaxMP, &s.MaxMP},
	}
}
