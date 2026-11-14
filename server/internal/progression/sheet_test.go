package progression_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/gamedata/gamedatatest"
	"finga/internal/kernel"
	"finga/internal/progression"
)

func TestASheetShowsTheTotalsAndWhatTheEquipmentAdds(t *testing.T) {
	data := gamedatatest.Shipped(t)
	base := data.StartClass().BaseStats
	worn := progression.ItemStats{PhysicalAttack: 10, PhysicalDefense: 3, STR: 4, CritRate: 15, MaxHP: 30}
	got := progression.SheetOf(data.StatRules(), kernel.Class("warrior"), 1, base, worn, progression.ItemStats{})

	if want := progression.AddStats(base, gamedata.Stats{STR: 4}); got.Primary != want || got.PrimaryWorn != (gamedata.Stats{STR: 4}) {
		t.Fatalf("primary %+v from worn %+v, want %+v and STR 4", got.Primary, got.PrimaryWorn, want)
	}
	assertDerived(t, got.Derived, progression.DeriveStats(data.StatRules(), kernel.Class("warrior"), 1, base, worn))
	if got.DerivedWorn.PhysicalAttack != 10+1.5*4 || got.DerivedWorn.MaxHP != 30+2*4 || got.DerivedWorn.PhysicalDefense != 3 {
		t.Fatalf("equipment's share %+v; want STR's physical attack and max HP on top of the sword's 10 and the 30, and the armor's 3 defense", got.DerivedWorn)
	}
}

// At its limit, a worn rate adds nothing, and the sheet says so.
func TestARateAtItsLimitShowsWhatTheEquipmentReallyAdds(t *testing.T) {
	got := progression.SheetOf(gamedatatest.Shipped(t).StatRules(), kernel.Class("bowman"), 1, gamedata.Stats{AGI: 100000}, progression.ItemStats{MoveSpeed: 200}, progression.ItemStats{})
	if got.Derived.MoveSpeed != 0.3 || math.Abs(got.DerivedWorn.MoveSpeed-(0.3-0.3*100000.0/100200)) > 1e-12 {
		t.Fatalf("move speed %v, the equipment's share %v; want the 0.3 limit and only what was left below it", got.Derived.MoveSpeed, got.DerivedWorn.MoveSpeed)
	}
}

// Each share is the total less the total without it, never below nothing.
func TestTheEquipmentsAndTheSkillsShareIsTheTotalLessTheTotalWithoutIt(t *testing.T) {
	data := gamedatatest.Shipped(t)
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom(classes).Draw(t, "class")
		own, level, worn, skills := drawStats(t, 200, "own"), rapid.IntRange(1, 30).Draw(t, "level"), drawWorn(t, 50), drawWorn(t, 50)
		got := progression.SheetOf(data.StatRules(), class, level, own, worn, skills)
		for _, c := range []struct {
			name    string
			share   progression.Derived
			without progression.ItemStats
		}{{"equipment", got.DerivedWorn, skills}, {"skills", got.DerivedSkills, worn}} {
			rest := progression.DeriveStats(data.StatRules(), class, level, own, c.without)
			total, without, share := rated(got.Derived, rest), rated(rest, rest), rated(c.share, c.share)
			for k := range total {
				if math.Abs(total[k].got-without[k].got-share[k].got) > 1e-9 || share[k].got < 0 {
					t.Fatalf("%s's share of %s: total %v, without %v, share %v", c.name, total[k].name, total[k].got, without[k].got, share[k].got)
				}
			}
		}
		if got.PrimaryWorn != worn.Primary() || got.PrimarySkills != skills.Primary() || got.Primary != progression.AddStats(own, worn.Plus(skills).Primary()) {
			t.Fatalf("primary %+v, worn %+v, skills %+v", got.Primary, got.PrimaryWorn, got.PrimarySkills)
		}
	})
}

// A passive skill adds its stage's bonus at its points, rounded to whole item units; with no points, nothing.
func TestPassivesAddTheirStagesBonus(t *testing.T) {
	data := gamedatatest.Shipped(t)
	wind, _ := data.Skill("second_wind")
	strike, _ := data.Skill("strike")
	for _, tt := range []struct {
		name    string
		learned []progression.Learned
		want    progression.ItemStats
	}{
		{"not learned", []progression.Learned{{Skill: wind}}, progression.ItemStats{}},
		{"a point", []progression.Learned{{Skill: wind, Points: 1}}, progression.ItemStats{HPRegen: 6}},
		{"the last point of the first stage", []progression.Learned{{Skill: wind, Points: 9}}, progression.ItemStats{HPRegen: 14}},
		{"evolved", []progression.Learned{{Skill: wind, Points: 10}}, progression.ItemStats{HPRegen: 20}},
		{"an active skill adds nothing", []progression.Learned{{Skill: strike, Points: 30}}, progression.ItemStats{}},
	} {
		if got := progression.Passives(tt.learned); got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
	if got := progression.BonusAt(gamedata.StatSet[gamedata.Scaled]{CritRate: gamedata.Scaled{Base: 1, PerPoint: 0.25}}, 2); got != (progression.ItemStats{CritRate: 2}) {
		t.Fatalf("1 + 0.25 × 2 = %+v, want it rounded to 2", got)
	}
}
