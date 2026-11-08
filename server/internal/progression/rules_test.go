package progression_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

// Expected values are worked out by hand from the formulas and classes.yaml.
func TestDeriveStats(t *testing.T) {
	data := loadData(t)
	tests := []struct {
		name  string
		class kernel.Class
		level int
		want  progression.Derived
	}{
		{
			"warrior level 1", kernel.Warrior, 1,
			progression.Derived{MaxHP: 204, MaxMP: 28, AttackPower: 21, Accuracy: 21, Evasion: 9, Defense: 15, CritRate: 0.06, CritDamage: 0.525},
		},
		{
			"warrior level 10", kernel.Warrior, 10,
			progression.Derived{MaxHP: 636, MaxMP: 64, AttackPower: 66.9, Accuracy: 34.5, Evasion: 9, Defense: 33, CritRate: 0.06, CritDamage: 0.525},
		},
		{
			"warrior level 30", kernel.Warrior, 30,
			progression.Derived{MaxHP: 1596, MaxMP: 144, AttackPower: 168.9, Accuracy: 64.5, Evasion: 9, Defense: 73, CritRate: 0.06, CritDamage: 0.525},
		},
		{
			"bowman level 1", kernel.Bowman, 1,
			progression.Derived{MaxHP: 105, MaxMP: 33, AttackPower: 21, Accuracy: 29.4, Evasion: 18, Defense: 6, CritRate: 0.08, CritDamage: 0.596},
		},
		{
			"bowman level 10", kernel.Bowman, 10,
			progression.Derived{MaxHP: 231, MaxMP: 78, AttackPower: 66.9, Accuracy: 61.8, Evasion: 36, Defense: 6, CritRate: 0.134, CritDamage: 0.812},
		},
		{
			"bowman level 30", kernel.Bowman, 30,
			progression.Derived{MaxHP: 511, MaxMP: 178, AttackPower: 168.9, Accuracy: 133.8, Evasion: 76, Defense: 6, CritRate: 0.254, CritDamage: 1.292},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, _ := data.Class(tt.class)
			formulas, _ := data.Formulas(tt.class)
			stats := class.BaseStats
			for range tt.level - 1 {
				stats = progression.AddStats(stats, class.LevelUpPoints)
			}
			got := progression.DeriveStats(formulas, stats, tt.level, progression.ItemStats{})
			assertDerived(t, got, tt.want)
		})
	}

	t.Run("worn flat bonuses add to each derived stat", func(t *testing.T) {
		class, _ := data.Class(kernel.Warrior)
		formulas, _ := data.Formulas(kernel.Warrior)
		worn := progression.ItemStats{Attack: 10, Defense: 3, Accuracy: 4, Evasion: 2, CritRate: 15, CritDamage: 50, MaxHP: 30, MaxMP: 7}
		got := progression.DeriveStats(formulas, class.BaseStats, 1, worn)
		assertDerived(t, got, progression.Derived{MaxHP: 234, MaxMP: 35, AttackPower: 31, Accuracy: 25, Evasion: 11, Defense: 18, CritRate: 0.075, CritDamage: 0.575})
	})

	t.Run("worn primary stats feed the formulas through the class's coefficients", func(t *testing.T) {
		class, _ := data.Class(kernel.Warrior)
		formulas, _ := data.Formulas(kernel.Warrior)
		got := progression.DeriveStats(formulas, class.BaseStats, 1, progression.ItemStats{STR: 4, VIT: 2})
		want := progression.DeriveStats(formulas, progression.AddStats(class.BaseStats, gamedata.Stats{STR: 4, VIT: 2}), 1, progression.ItemStats{})
		assertDerived(t, got, want)
		if got.AttackPower <= 21 {
			t.Fatalf("attack power %v; worn STR should raise a warrior's", got.AttackPower)
		}
	})

	t.Run("a worn crit rate stops at the cap too", func(t *testing.T) {
		formulas, _ := data.Formulas(kernel.Bowman)
		if got := progression.DeriveStats(formulas, gamedata.Stats{AGI: 150}, 1, progression.ItemStats{CritRate: 200}); got.CritRate != 0.6 {
			t.Fatalf("crit rate %v, want the 0.6 cap", got.CritRate)
		}
	})

	t.Run("crit rate stops at the 60% cap", func(t *testing.T) {
		formulas, _ := data.Formulas(kernel.Bowman)
		got := progression.DeriveStats(formulas, gamedata.Stats{AGI: 200}, 1, progression.ItemStats{}) // 0.05 + 0.6 = 0.65 uncapped
		if got.CritRate != 0.6 {
			t.Fatalf("crit rate %v, want 0.6", got.CritRate)
		}
	})

	t.Run("no equipment gives penetration", func(t *testing.T) {
		formulas, _ := data.Formulas(kernel.Warrior)
		worn := progression.ItemStats{Attack: 50, Defense: 50, STR: 50, DEX: 50, INT: 50, AGI: 50, VIT: 50, Accuracy: 50, Evasion: 50, CritRate: 50, CritDamage: 50, MaxHP: 50, MaxMP: 50}
		if got := progression.DeriveStats(formulas, gamedata.Stats{}, 1, worn); got.Penetration != 0 {
			t.Fatalf("penetration %v from equipment", got.Penetration)
		}
	})
}

func assertDerived(t *testing.T, got, want progression.Derived) {
	t.Helper()
	if got.MaxHP != want.MaxHP || got.MaxMP != want.MaxMP {
		t.Fatalf("HP/MP %d/%d, want %d/%d", got.MaxHP, got.MaxMP, want.MaxHP, want.MaxMP)
	}
	for _, f := range []struct {
		name      string
		got, want float64
	}{
		{"attack power", got.AttackPower, want.AttackPower},
		{"accuracy", got.Accuracy, want.Accuracy},
		{"evasion", got.Evasion, want.Evasion},
		{"defense", got.Defense, want.Defense},
		{"crit rate", got.CritRate, want.CritRate},
		{"crit damage", got.CritDamage, want.CritDamage},
		{"penetration", got.Penetration, want.Penetration},
	} {
		if math.Abs(f.got-f.want) > 1e-9 {
			t.Fatalf("%s = %v, want %v", f.name, f.got, f.want)
		}
	}
}

func TestAwardExp(t *testing.T) {
	data := loadData(t) // exp_to_next: 1→20, 2→65, 3→129, 4→211 ... cap 30
	warrior, _ := data.Class(kernel.Warrior)
	preset := func(int) gamedata.Stats { return warrior.LevelUpPoints }
	base := warrior.BaseStats
	tests := []struct {
		name       string
		start      progression.Progress
		amount     int64
		wantAfter  progression.Progress
		wantLevels int
		wantStats  gamedata.Stats
	}{
		{"below the threshold accumulates", progression.Progress{Level: 1, Exp: 5}, 10, progression.Progress{Level: 1, Exp: 15}, 0, base},
		{"exactly at the threshold levels up with 0 carried", progression.Progress{Level: 1, Exp: 5}, 15, progression.Progress{Level: 2, Exp: 0}, 1, progression.AddStats(base, warrior.LevelUpPoints)},
		{"extra EXP carries over", progression.Progress{Level: 1, Exp: 0}, 30, progression.Progress{Level: 2, Exp: 10}, 1, progression.AddStats(base, warrior.LevelUpPoints)},
		{"several levels at once", progression.Progress{Level: 1, Exp: 0}, 20 + 65 + 129 + 7, progression.Progress{Level: 4, Exp: 7}, 3, gamedata.Stats{STR: 21, DEX: 5, INT: 4, AGI: 5, VIT: 16}},
		{"reaching the cap discards the remainder", progression.Progress{Level: 29, Exp: 6000}, 1000, progression.Progress{Level: 30, Exp: 0}, 1, progression.AddStats(base, warrior.LevelUpPoints)},
		{"at the cap nothing accumulates", progression.Progress{Level: 30, Exp: 0}, 5000, progression.Progress{Level: 30, Exp: 0}, 0, base},
		{"zero EXP changes nothing", progression.Progress{Level: 3, Exp: 50}, 0, progression.Progress{Level: 3, Exp: 50}, 0, base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := progression.AwardExp(data, tt.start, tt.amount, base, preset)
			if got.Progress != tt.wantAfter || got.LevelsGained != tt.wantLevels {
				t.Fatalf("got %+v (+%d), want %+v (+%d)", got.Progress, got.LevelsGained, tt.wantAfter, tt.wantLevels)
			}
			if got.Stats != tt.wantStats {
				t.Fatalf("stats %+v, want %+v", got.Stats, tt.wantStats)
			}
		})
	}
	t.Run("the allocation plan is asked once per new level, with that level", func(t *testing.T) {
		var asked []int
		plan := func(level int) gamedata.Stats { asked = append(asked, level); return gamedata.Stats{INT: 5} }
		got := progression.AwardExp(data, progression.Progress{Level: 1}, 20+65, base, plan)
		if len(asked) != 2 || asked[0] != 2 || asked[1] != 3 {
			t.Fatalf("asked for levels %v, want [2 3]", asked)
		}
		if got.Stats.INT != base.INT+10 {
			t.Fatalf("INT %d, want %d", got.Stats.INT, base.INT+10)
		}
	})
}

func TestDeathPenalty(t *testing.T) {
	data := loadData(t) // level 5 needs 309: 5% is 15.45, floored to 15
	tests := []struct {
		name   string
		start  progression.Progress
		inTown bool
		want   progression.Progress
	}{
		{"mid-level loses 5% of the level's requirement, floored", progression.Progress{Level: 5, Exp: 100}, false, progression.Progress{Level: 5, Exp: 85}},
		{"near the start of a level stops at 0", progression.Progress{Level: 5, Exp: 7}, false, progression.Progress{Level: 5, Exp: 0}},
		{"at exactly 0 stays 0", progression.Progress{Level: 5, Exp: 0}, false, progression.Progress{Level: 5, Exp: 0}},
		{"in town there is no penalty", progression.Progress{Level: 5, Exp: 100}, true, progression.Progress{Level: 5, Exp: 100}},
		{"level 1 loses 5% of 20, which floors to 1", progression.Progress{Level: 1, Exp: 10}, false, progression.Progress{Level: 1, Exp: 9}},
		{"at the cap there is nothing to lose", progression.Progress{Level: 30, Exp: 0}, false, progression.Progress{Level: 30, Exp: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progression.DeathPenalty(data, tt.start, tt.inTown); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAddStats(t *testing.T) {
	got := progression.AddStats(gamedata.Stats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5}, gamedata.Stats{STR: 10, VIT: 20})
	if got != (gamedata.Stats{STR: 11, DEX: 2, INT: 3, AGI: 4, VIT: 25}) {
		t.Fatalf("got %+v", got)
	}
}

func loadData(t *testing.T) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Load("../../data")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Whatever is worn, no derived stat drops, the flat bonuses add exactly, and the caps hold.
func TestWearingNeverLowersAStat(t *testing.T) {
	data := loadData(t)
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom([]kernel.Class{kernel.Warrior, kernel.Bowman}).Draw(t, "class")
		formulas, _ := data.Formulas(class)
		stat := rapid.IntRange(0, 200)
		base := gamedata.Stats{STR: stat.Draw(t, "str"), DEX: stat.Draw(t, "dex"), INT: stat.Draw(t, "int"), AGI: stat.Draw(t, "agi"), VIT: stat.Draw(t, "vit")}
		level := rapid.IntRange(1, 30).Draw(t, "level")
		bonus := rapid.IntRange(0, 500)
		worn := progression.ItemStats{
			Attack: bonus.Draw(t, "attack"), Defense: bonus.Draw(t, "defense"),
			STR: bonus.Draw(t, "w_str"), DEX: bonus.Draw(t, "w_dex"), INT: bonus.Draw(t, "w_int"), AGI: bonus.Draw(t, "w_agi"), VIT: bonus.Draw(t, "w_vit"),
			Accuracy: bonus.Draw(t, "accuracy"), Evasion: bonus.Draw(t, "evasion"), CritRate: bonus.Draw(t, "crit_rate"), CritDamage: bonus.Draw(t, "crit_damage"),
			MaxHP: bonus.Draw(t, "max_hp"), MaxMP: bonus.Draw(t, "max_mp"),
		}
		bare := progression.DeriveStats(formulas, base, level, progression.ItemStats{})
		dressed := progression.DeriveStats(formulas, base, level, worn)
		for _, f := range []struct {
			name       string
			bare, worn float64
		}{
			{"max HP", float64(bare.MaxHP), float64(dressed.MaxHP)}, {"max MP", float64(bare.MaxMP), float64(dressed.MaxMP)},
			{"attack power", bare.AttackPower, dressed.AttackPower}, {"accuracy", bare.Accuracy, dressed.Accuracy},
			{"evasion", bare.Evasion, dressed.Evasion}, {"defense", bare.Defense, dressed.Defense},
			{"crit rate", bare.CritRate, dressed.CritRate}, {"crit damage", bare.CritDamage, dressed.CritDamage},
		} {
			if f.worn < f.bare {
				t.Fatalf("%s fell from %v to %v with %+v worn", f.name, f.bare, f.worn, worn)
			}
		}
		if dressed.CritRate > formulas.CritRate.Cap || dressed.Penetration > formulas.Penetration.Cap {
			t.Fatalf("crit rate %v, penetration %v past their caps", dressed.CritRate, dressed.Penetration)
		}
		flat := progression.DeriveStats(formulas, base, level, progression.ItemStats{Attack: worn.Attack, MaxHP: worn.MaxHP})
		if flat.MaxHP != bare.MaxHP+worn.MaxHP || math.Abs(flat.AttackPower-bare.AttackPower-float64(worn.Attack)) > 1e-9 {
			t.Fatalf("flat bonuses: max HP %d from %d, attack %v from %v, with %+v", flat.MaxHP, bare.MaxHP, flat.AttackPower, bare.AttackPower, worn)
		}
	})
}
