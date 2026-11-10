package progression

import (
	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// Sheet is what the character window shows: the totals, and the equipment's share of each, the totals less those without it, so a rate held to its limit shows what the equipment really adds.
type Sheet struct {
	Primary     gamedata.Stats `json:"primary"`
	PrimaryWorn gamedata.Stats `json:"primary_worn"`
	Derived     Derived        `json:"derived"`
	DerivedWorn Derived        `json:"derived_worn"`
}

// SheetOf is the sheet of a character of class at level, with own primary stats, wearing worn.
func SheetOf(r gamedata.StatRules, class kernel.Class, level int, own gamedata.Stats, worn ItemStats) Sheet {
	with, without := DeriveStats(r, class, level, own, worn), DeriveStats(r, class, level, own, ItemStats{})
	return Sheet{
		Primary:     AddStats(own, worn.Primary()),
		PrimaryWorn: worn.Primary(),
		Derived:     with,
		DerivedWorn: with.minus(without),
	}
}
