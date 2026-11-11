package progression

import (
	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// Sheet is what the character window shows: the totals, and the equipment's and the skills' share of each, the totals less those without it, so a rate held to its limit shows what each really adds.
type Sheet struct {
	Primary       gamedata.Stats `json:"primary"`
	PrimaryWorn   gamedata.Stats `json:"primary_worn"`
	PrimarySkills gamedata.Stats `json:"primary_skills"`
	Derived       Derived        `json:"derived"`
	DerivedWorn   Derived        `json:"derived_worn"`
	DerivedSkills Derived        `json:"derived_skills"`
}

// SheetOf is the sheet of a character of class at level, with own primary stats, wearing worn, and given skills by its skills.
func SheetOf(r gamedata.StatRules, class kernel.Class, level int, own gamedata.Stats, worn, skills ItemStats) Sheet {
	with := DeriveStats(r, class, level, own, worn.Plus(skills))
	return Sheet{
		Primary:       AddStats(own, worn.Plus(skills).Primary()),
		PrimaryWorn:   worn.Primary(),
		PrimarySkills: skills.Primary(),
		Derived:       with,
		DerivedWorn:   with.minus(DeriveStats(r, class, level, own, skills)),
		DerivedSkills: with.minus(DeriveStats(r, class, level, own, worn)),
	}
}
