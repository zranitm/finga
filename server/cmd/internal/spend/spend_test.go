package spend

import (
	"maps"
	"testing"

	"finga/internal/gamedata"
)

func TestPointsGoWhereThePromotionAsksFirst(t *testing.T) {
	split := gamedata.Stats{STR: 2, DEX: 2, AGI: 1} // the Bowman's: 25 DEX at level 10 on its own
	if got := Stats(10, gamedata.Stats{DEX: 25}, gamedata.Stats{DEX: 30}, split); got != (gamedata.Stats{STR: 2, DEX: 7, AGI: 1}) {
		t.Errorf("10 points with 5 DEX to go spend %+v", got)
	}
	if got := Stats(3, gamedata.Stats{DEX: 30}, gamedata.Stats{DEX: 30}, split); got != (gamedata.Stats{}) {
		t.Errorf("3 points, the need met, spend %+v; want them kept for a whole split", got)
	}
	if got := Skills(5, map[string]int32{"strike": 8}, map[string]int{"strike": 10}, "strike"); !maps.Equal(got, map[string]int32{"strike": 5}) {
		t.Errorf("skill points before promotion go %v", got)
	}
	if got := Skills(5, map[string]int32{"strike": 10}, nil, "slash_blast"); !maps.Equal(got, map[string]int32{"slash_blast": 5}) {
		t.Errorf("skill points after promotion go %v", got)
	}
}

func TestTheMainSkillIsThePromotionsUntilPromotedThenTheClasss(t *testing.T) {
	warrior := gamedata.Class{Skills: []string{"slash_blast", "iron_body"}, Promotion: gamedata.Promotion{Skills: map[string]int{"strike": 10}}}
	if got := MainSkill(warrior, false); got != "strike" {
		t.Errorf("a Beginner bound for Warrior puts the rest in %q; want strike", got)
	}
	if got := MainSkill(warrior, true); got != "slash_blast" {
		t.Errorf("a Warrior puts the rest in %q; want slash_blast", got)
	}
	if got := MainSkill(gamedata.Class{}, true); got != "" {
		t.Errorf("a class with no skills puts the rest in %q; want none", got)
	}
}
