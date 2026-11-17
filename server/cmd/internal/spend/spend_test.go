package spend

import (
	"maps"
	"slices"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
)

func TestPointsGoWhereThePromotionAsksFirst(t *testing.T) {
	split := gamedata.Stats{STR: 2, DEX: 2, AGI: 1} // the Bowman's: 25 DEX at level 10 on its own
	if got := Stats(10, gamedata.Stats{DEX: 25}, gamedata.Stats{DEX: 30}, split); got != (gamedata.Stats{STR: 2, DEX: 7, AGI: 1}) {
		t.Errorf("10 points with 5 DEX to go spend %+v", got)
	}
	if got := Stats(3, gamedata.Stats{DEX: 30}, gamedata.Stats{DEX: 30}, split); got != (gamedata.Stats{}) {
		t.Errorf("3 points, the need met, spend %+v; want them kept for a whole split", got)
	}
	if got := Skills(5, map[string]int32{"strike": 8}, map[string]int{"strike": 10}, nil, "strike"); !maps.Equal(got, map[string]int32{"strike": 5}) {
		t.Errorf("skill points before promotion go %v", got)
	}
	if got := Skills(5, map[string]int32{"strike": 10}, nil, nil, "slash_blast"); !maps.Equal(got, map[string]int32{"slash_blast": 5}) {
		t.Errorf("skill points after promotion go %v", got)
	}
}

// C17: after the promotion's asks, a point goes into each opened skill with none, in order, while points last; the rest goes to the main skill.
func TestEachOpenedSkillGetsAPoint(t *testing.T) {
	opened := []string{"strike", "throw", "lunge", "roll_back"}
	if got := Skills(14, map[string]int32{"throw": 2}, map[string]int{"strike": 10}, opened, "strike"); !maps.Equal(got, map[string]int32{"strike": 12, "lunge": 1, "roll_back": 1}) {
		t.Errorf("14 points go %v; want strike's 10, a point each in lunge and roll_back, and the 2 left in strike", got)
	}
	if got := Skills(11, nil, map[string]int{"strike": 10}, opened, "strike"); !maps.Equal(got, map[string]int32{"strike": 10, "throw": 1}) {
		t.Errorf("11 points go %v; want strike's 10 and one into the first opened", got)
	}
}

// C17: the skills a character may put points in at its level, the root's first: a level 12 Warrior has the Beginner's and Charge, not Ground Slam at 15, nor a granted break-free, Break Free or Unyielding.
func TestOpenedSkillsAreTheLinesAtTheLevel(t *testing.T) {
	d := gamedatatest.Shipped(t)
	got := Opened(d, "warrior", 12)
	want := []string{"strike", "throw", "spark", "second_wind", "lunge", "roll_back", "slash_blast", "iron_body", "charge", "toughness"}
	if !slices.Equal(got, want) {
		t.Errorf("a level 12 Warrior's opened skills %v, want %v", got, want)
	}
	if got := Opened(d, "beginner", 4); slices.Contains(got, "roll_back") || !slices.Contains(got, "lunge") {
		t.Errorf("a level 4 Beginner's opened skills %v; want Lunge at 3 and not Roll Back at 5", got)
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
