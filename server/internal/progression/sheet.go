package progression

import "finga/internal/gamedata"

// Sheet is what the character window shows: the totals, and the equipment's share of each, the totals less those without it, so a capped rate shows what the equipment really adds.
type Sheet struct {
	Primary     gamedata.Stats
	PrimaryWorn gamedata.Stats
	Derived     Derived
	DerivedWorn Derived
}

// SheetOf is the sheet of a character with base stats at level, wearing worn.
func SheetOf(f gamedata.Formulas, base gamedata.Stats, level int, worn ItemStats) Sheet {
	with, without := DeriveStats(f, base, level, worn), DeriveStats(f, base, level, ItemStats{})
	return Sheet{
		Primary:     AddStats(base, worn.Primary()),
		PrimaryWorn: worn.Primary(),
		Derived:     with,
		DerivedWorn: Derived{
			MaxHP: with.MaxHP - without.MaxHP, MaxMP: with.MaxMP - without.MaxMP,
			AttackPower: with.AttackPower - without.AttackPower, Accuracy: with.Accuracy - without.Accuracy,
			Evasion: with.Evasion - without.Evasion, Defense: with.Defense - without.Defense,
			CritRate: with.CritRate - without.CritRate, CritDamage: with.CritDamage - without.CritDamage,
			Penetration: with.Penetration - without.Penetration,
		},
	}
}
