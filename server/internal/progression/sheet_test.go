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
	formulas, _ := data.Formulas(kernel.Warrior)
	worn := progression.ItemStats{Attack: 10, Defense: 3, STR: 4, CritRate: 15, MaxHP: 30}
	got := progression.SheetOf(formulas, class.BaseStats, 1, worn)

	if want := progression.AddStats(class.BaseStats, gamedata.Stats{STR: 4}); got.Primary != want || got.PrimaryWorn != (gamedata.Stats{STR: 4}) {
		t.Fatalf("primary %+v from worn %+v, want %+v and STR 4", got.Primary, got.PrimaryWorn, want)
	}
	assertDerived(t, got.Derived, progression.DeriveStats(formulas, class.BaseStats, 1, worn))
	bare := progression.DeriveStats(formulas, class.BaseStats, 1, progression.ItemStats{})
	if got.DerivedWorn.AttackPower <= 10 || got.DerivedWorn.MaxHP != got.Derived.MaxHP-bare.MaxHP || got.DerivedWorn.Defense != 3 {
		t.Fatalf("equipment's share %+v; want STR's attack power on top of the sword's 10, and the armor's 3 defense", got.DerivedWorn)
	}
}

// At the cap, a worn crit rate adds nothing, and the sheet says so.
func TestACappedRateShowsWhatTheEquipmentReallyAdds(t *testing.T) {
	formulas, _ := loadData(t).Formulas(kernel.Bowman)
	got := progression.SheetOf(formulas, gamedata.Stats{AGI: 999}, 1, progression.ItemStats{CritRate: 200})
	if got.Derived.CritRate != 0.6 || got.DerivedWorn.CritRate != 0 {
		t.Fatalf("crit rate %v, the equipment's share %v; want the 0.6 cap and no share", got.Derived.CritRate, got.DerivedWorn.CritRate)
	}
}

func TestTheEquipmentsShareIsTheTotalLessTheTotalWithoutIt(t *testing.T) {
	data := loadData(t)
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom([]kernel.Class{kernel.Warrior, kernel.Bowman}).Draw(t, "class")
		formulas, _ := data.Formulas(class)
		stat, bonus := rapid.IntRange(0, 200), rapid.IntRange(0, 50)
		base := gamedata.Stats{STR: stat.Draw(t, "str"), DEX: stat.Draw(t, "dex"), INT: stat.Draw(t, "int"), AGI: stat.Draw(t, "agi"), VIT: stat.Draw(t, "vit")}
		worn := progression.ItemStats{
			Attack: bonus.Draw(t, "attack"), Defense: bonus.Draw(t, "defense"), STR: bonus.Draw(t, "worn str"), AGI: bonus.Draw(t, "worn agi"),
			CritRate: bonus.Draw(t, "crit rate"), MaxHP: bonus.Draw(t, "max hp"),
		}
		level := rapid.IntRange(1, 30).Draw(t, "level")
		got := progression.SheetOf(formulas, base, level, worn)
		bare := progression.DeriveStats(formulas, base, level, progression.ItemStats{})
		for _, f := range []struct {
			name           string
			total, without float64
			share          float64
		}{
			{"attack power", got.Derived.AttackPower, bare.AttackPower, got.DerivedWorn.AttackPower},
			{"defense", got.Derived.Defense, bare.Defense, got.DerivedWorn.Defense},
			{"crit rate", got.Derived.CritRate, bare.CritRate, got.DerivedWorn.CritRate},
			{"max hp", float64(got.Derived.MaxHP), float64(bare.MaxHP), float64(got.DerivedWorn.MaxHP)},
		} {
			if math.Abs(f.total-f.without-f.share) > 1e-9 || f.share < 0 {
				t.Fatalf("%s: total %v, without %v, share %v", f.name, f.total, f.without, f.share)
			}
		}
	})
}
