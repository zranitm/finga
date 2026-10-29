package progression_test

import (
	"math"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

// Expected values worked out by hand from page 5 and the base stats in
// classes.yaml. Warrior starts STR 12 DEX 5 INT 4 AGI 5 VIT 10 and gains
// STR 3 VIT 2 per level; Bowman starts STR 5 DEX 12 INT 4 AGI 10 VIT 5 and
// gains DEX 3 AGI 2.
func TestDeriveStats(t *testing.T) {
	data := loadData(t)
	tests := []struct {
		name  string
		class kernel.Class
		level int
		want  progression.Derived
	}{
		{
			// MaxHP 60+12·10+2·12 = 204; MaxMP 20+2·4 = 28; AP 1.5·12+0.3·10 = 21;
			// Acc 10+5+0.5·12 = 21; Eva 5+0.8·5 = 9; Crit 0.05+0.002·5 = 0.06;
			// CritDmg 0.5+0.005·5 = 0.525; Def 5+10 = 15.
			"warrior level 1", kernel.Warrior, 1,
			progression.Derived{MaxHP: 204, MaxMP: 28, AttackPower: 21, Accuracy: 21, Evasion: 9, Defense: 15, CritRate: 0.06, CritDamage: 0.525},
		},
		{
			// 9 level-ups: STR 39, VIT 28. MaxHP 60+336+78+18·9 = 636; MaxMP 20+8+4·9 = 64;
			// AP 58.5+8.4 = 66.9; Acc 10+5+19.5 = 34.5; Def 5+28 = 33.
			"warrior level 10", kernel.Warrior, 10,
			progression.Derived{MaxHP: 636, MaxMP: 64, AttackPower: 66.9, Accuracy: 34.5, Evasion: 9, Defense: 33, CritRate: 0.06, CritDamage: 0.525},
		},
		{
			// 29 level-ups: STR 99, VIT 68. MaxHP 60+816+198+522 = 1596; MaxMP 20+8+116 = 144;
			// AP 148.5+20.4 = 168.9; Acc 10+5+49.5 = 64.5; Def 73.
			"warrior level 30", kernel.Warrior, 30,
			progression.Derived{MaxHP: 1596, MaxMP: 144, AttackPower: 168.9, Accuracy: 64.5, Evasion: 9, Defense: 73, CritRate: 0.06, CritDamage: 0.525},
		},
		{
			// MaxHP 50+9·5+1·10 = 105; MaxMP 25+8 = 33; AP 1.5·12+0.3·10 = 21; Acc 15+1.2·12 = 29.4;
			// Eva 8+10 = 18; Crit 0.05+0.003·10 = 0.08; CritDmg 0.5+0.008·12 = 0.596; Def 3+0.6·5 = 6.
			"bowman level 1", kernel.Bowman, 1,
			progression.Derived{MaxHP: 105, MaxMP: 33, AttackPower: 21, Accuracy: 29.4, Evasion: 18, Defense: 6, CritRate: 0.08, CritDamage: 0.596},
		},
		{
			// DEX 39, AGI 28. MaxHP 50+45+28+108 = 231; MaxMP 25+8+45 = 78; AP 58.5+8.4 = 66.9;
			// Acc 15+46.8 = 61.8; Eva 36; Crit 0.05+0.084 = 0.134; CritDmg 0.5+0.312 = 0.812.
			"bowman level 10", kernel.Bowman, 10,
			progression.Derived{MaxHP: 231, MaxMP: 78, AttackPower: 66.9, Accuracy: 61.8, Evasion: 36, Defense: 6, CritRate: 0.134, CritDamage: 0.812},
		},
		{
			// DEX 99, AGI 68. MaxHP 50+45+68+348 = 511; MaxMP 25+8+145 = 178; AP 168.9;
			// Acc 15+118.8 = 133.8; Eva 76; Crit 0.254; CritDmg 1.292.
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
			got := progression.DeriveStats(formulas, stats, tt.level, progression.Equipment{})
			assertDerived(t, got, tt.want)
		})
	}

	t.Run("equipment adds to attack power and defense", func(t *testing.T) {
		class, _ := data.Class(kernel.Warrior)
		formulas, _ := data.Formulas(kernel.Warrior)
		got := progression.DeriveStats(formulas, class.BaseStats, 1, progression.Equipment{WeaponAttack: 10, ArmorDefense: 3})
		if got.AttackPower != 31 || got.Defense != 18 {
			t.Fatalf("attack %v defense %v, want 31 and 18", got.AttackPower, got.Defense)
		}
	})

	t.Run("crit rate stops at the 60% cap", func(t *testing.T) {
		formulas, _ := data.Formulas(kernel.Bowman)
		got := progression.DeriveStats(formulas, gamedata.Stats{AGI: 200}, 1, progression.Equipment{}) // 0.05 + 0.6 = 0.65 uncapped
		if got.CritRate != 0.6 {
			t.Fatalf("crit rate %v, want 0.6", got.CritRate)
		}
	})

	t.Run("penetration is equipment only and capped at 50%", func(t *testing.T) {
		formulas, _ := data.Formulas(kernel.Warrior)
		got := progression.DeriveStats(formulas, gamedata.Stats{STR: 999}, 30, progression.Equipment{Penetration: 0.7})
		if got.Penetration != 0.5 {
			t.Fatalf("penetration %v, want 0.5", got.Penetration)
		}
		if got := progression.DeriveStats(formulas, gamedata.Stats{}, 1, progression.Equipment{}); got.Penetration != 0 {
			t.Fatalf("penetration without equipment %v", got.Penetration)
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
