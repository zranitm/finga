package progression_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

func TestASheetShowsTheTotalsAndWhatTheEquipmentAdds(t *testing.T) {
	data := loadData(t)
	class, _ := data.Class(kernel.Warrior)
	worn := progression.ItemStats{PhysicalAttack: 10, PhysicalDefense: 3, STR: 4, CritRate: 15, MaxHP: 30}
	got := progression.SheetOf(data.StatRules(), kernel.Warrior, 1, class.BaseStats, worn)

	if want := progression.AddStats(class.BaseStats, gamedata.Stats{STR: 4}); got.Primary != want || got.PrimaryWorn != (gamedata.Stats{STR: 4}) {
		t.Fatalf("primary %+v from worn %+v, want %+v and STR 4", got.Primary, got.PrimaryWorn, want)
	}
	assertDerived(t, got.Derived, progression.DeriveStats(data.StatRules(), kernel.Warrior, 1, class.BaseStats, worn))
	if got.DerivedWorn.PhysicalAttack != 10+1.5*4 || got.DerivedWorn.MaxHP != 30+2*4 || got.DerivedWorn.PhysicalDefense != 3 {
		t.Fatalf("equipment's share %+v; want STR's physical attack and max HP on top of the sword's 10 and the 30, and the armor's 3 defense", got.DerivedWorn)
	}
}

// At its limit, a worn rate adds nothing, and the sheet says so.
func TestARateAtItsLimitShowsWhatTheEquipmentReallyAdds(t *testing.T) {
	got := progression.SheetOf(loadData(t).StatRules(), kernel.Bowman, 1, gamedata.Stats{AGI: 100000}, progression.ItemStats{MoveSpeed: 200})
	if got.Derived.MoveSpeed != 0.3 || math.Abs(got.DerivedWorn.MoveSpeed-(0.3-0.3*100000.0/100200)) > 1e-12 {
		t.Fatalf("move speed %v, the equipment's share %v; want the 0.3 limit and only what was left below it", got.Derived.MoveSpeed, got.DerivedWorn.MoveSpeed)
	}
}

func TestTheEquipmentsShareIsTheTotalLessTheTotalWithoutIt(t *testing.T) {
	data := loadData(t)
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom(classes).Draw(t, "class")
		own, level, worn := drawStats(t, 200, "own"), rapid.IntRange(1, 30).Draw(t, "level"), drawWorn(t, 50)
		got := progression.SheetOf(data.StatRules(), class, level, own, worn)
		bare := progression.DeriveStats(data.StatRules(), class, level, own, progression.ItemStats{})
		total, without, share := rated(got.Derived, bare), rated(bare, bare), rated(got.DerivedWorn, got.DerivedWorn)
		for k := range total {
			if math.Abs(total[k].got-without[k].got-share[k].got) > 1e-9 || share[k].got < 0 {
				t.Fatalf("%s: total %v, without %v, share %v", total[k].name, total[k].got, without[k].got, share[k].got)
			}
		}
	})
}
