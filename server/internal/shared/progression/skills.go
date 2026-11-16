package progression

import (
	"math"

	"finga/internal/gamedata"
)

// Learned is a skill and the points a character has invested in it.
type Learned struct {
	Skill  gamedata.Skill
	Points int
}

// Stage is the stage the skill is in with its points.
func (l Learned) Stage() gamedata.Stage { return l.Skill.Stages[l.Skill.StageAt(l.Points)] }

// Passives sums what the passive skills with a point or more add, each at its stage and points, in the units items use.
func Passives(learned []Learned) ItemStats {
	var sum ItemStats
	for _, l := range learned {
		if l.Points > 0 && l.Skill.Kind == gamedata.Passive {
			sum = sum.Plus(BonusAt(l.Stage().Bonus, l.Points))
		}
	}
	return sum
}

// BonusAt is a bonus with points invested, each stat rounded to a whole number, as an item's roll is.
func BonusAt(b gamedata.StatSet[gamedata.Scaled], points int) ItemStats {
	var s ItemStats
	for _, f := range fieldsOf(b, &s) {
		*f.value = int(math.Round(f.in.At(points)))
	}
	return s
}
