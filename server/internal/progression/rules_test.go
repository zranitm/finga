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

// Expected values are worked out by hand from stats.yaml and classes.yaml: every character starts at the Beginner's 7 in each stat, and a warrior's auto-assign adds STR 3 and VIT 2 a level.
func TestDeriveStats(t *testing.T) {
	data := gamedatatest.Shipped(t)
	tests := []struct {
		name  string
		class kernel.Class
		level int
		want  progression.Derived
	}{
		{
			"warrior level 1", kernel.Class("warrior"), 1,
			progression.Derived{
				MaxHP: 60 + 2*7 + 10*7, MaxMP: 20 + 3*7,
				PhysicalAttack: 1.5*7 + 0.5*7, MagicAttack: 0.5*7 + 1.5*7, PhysicalDefense: 5 + 7, MagicDefense: 3 + 0.5*7 + 0.5*7,
				Accuracy: 10 + 7, Evasion: 5 + 7,
				CritRate: 0.05 + 0.55*7.0/207, CritDamage: 0.5 + 0.005*7,
				HPRegen: 0.05 * 7, MPRegen: 0.05 * 7,
				CooldownSpeed: 0.4 * 7.0 / 207, MoveSpeed: 0.3 * 7.0 / 207,
			},
		},
		{
			"warrior level 10, auto-assigned: STR 34 and VIT 25", kernel.Class("warrior"), 10,
			progression.Derived{
				MaxHP: 60 + 18*9 + 2*34 + 10*25, MaxMP: 20 + 4*9 + 3*7,
				PhysicalAttack: 1.5*34 + 0.5*7, MagicAttack: 0.5*7 + 1.5*7, PhysicalDefense: 5 + 25, MagicDefense: 3 + 0.5*7 + 0.5*25,
				Accuracy: 10 + 7, Evasion: 5 + 7,
				CritRate: 0.05 + 0.55*7.0/207, CritDamage: 0.5 + 0.005*7,
				HPRegen: 0.05 * 25, MPRegen: 0.05 * 7,
				CooldownSpeed: 0.4 * 7.0 / 207, MoveSpeed: 0.3 * 7.0 / 207,
			},
		},
		{
			"bowman level 1: the same stats, a bowman's bases", kernel.Class("bowman"), 1,
			progression.Derived{
				MaxHP: 50 + 2*7 + 10*7, MaxMP: 25 + 3*7,
				PhysicalAttack: 1.5*7 + 0.5*7, MagicAttack: 0.5*7 + 1.5*7, PhysicalDefense: 3 + 7, MagicDefense: 3 + 0.5*7 + 0.5*7,
				Accuracy: 15 + 7, Evasion: 8 + 7,
				CritRate: 0.05 + 0.55*7.0/207, CritDamage: 0.5 + 0.005*7,
				HPRegen: 0.05 * 7, MPRegen: 0.05 * 7,
				CooldownSpeed: 0.4 * 7.0 / 207, MoveSpeed: 0.3 * 7.0 / 207,
			},
		},
		{
			"beginner level 1: the bases midway between the four classes'", kernel.Class("beginner"), 1,
			progression.Derived{
				MaxHP: 51 + 2*7 + 10*7, MaxMP: 27 + 3*7,
				PhysicalAttack: 1.5*7 + 0.5*7, MagicAttack: 0.5*7 + 1.5*7, PhysicalDefense: 4 + 7, MagicDefense: 4 + 0.5*7 + 0.5*7,
				Accuracy: 12 + 7, Evasion: 8 + 7,
				CritRate: 0.05 + 0.55*7.0/207, CritDamage: 0.5 + 0.005*7,
				HPRegen: 0.05 * 7, MPRegen: 0.05 * 7,
				CooldownSpeed: 0.4 * 7.0 / 207, MoveSpeed: 0.3 * 7.0 / 207,
			},
		},
		{
			"thief level 1: the most evasion", kernel.Class("thief"), 1,
			progression.Derived{
				MaxHP: 55 + 2*7 + 10*7, MaxMP: 22 + 3*7,
				PhysicalAttack: 1.5*7 + 0.5*7, MagicAttack: 0.5*7 + 1.5*7, PhysicalDefense: 4 + 7, MagicDefense: 3 + 0.5*7 + 0.5*7,
				Accuracy: 13 + 7, Evasion: 15 + 7,
				CritRate: 0.05 + 0.55*7.0/207, CritDamage: 0.5 + 0.005*7,
				HPRegen: 0.05 * 7, MPRegen: 0.05 * 7,
				CooldownSpeed: 0.4 * 7.0 / 207, MoveSpeed: 0.3 * 7.0 / 207,
			},
		},
		{
			"magician level 1: the most MP and magic defense, the least HP", kernel.Class("magician"), 1,
			progression.Derived{
				MaxHP: 40 + 2*7 + 10*7, MaxMP: 40 + 3*7,
				PhysicalAttack: 1.5*7 + 0.5*7, MagicAttack: 0.5*7 + 1.5*7, PhysicalDefense: 2 + 7, MagicDefense: 8 + 0.5*7 + 0.5*7,
				Accuracy: 12 + 7, Evasion: 6 + 7,
				CritRate: 0.05 + 0.55*7.0/207, CritDamage: 0.5 + 0.005*7,
				HPRegen: 0.05 * 7, MPRegen: 0.05 * 7,
				CooldownSpeed: 0.4 * 7.0 / 207, MoveSpeed: 0.3 * 7.0 / 207,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, _ := data.Class(tt.class)
			own := data.StartClass().BaseStats
			for range tt.level - 1 {
				own = progression.AddStats(own, class.AutoAssign)
			}
			assertDerived(t, progression.DeriveStats(data.StatRules(), tt.class, tt.level, own, progression.ItemStats{}), tt.want)
		})
	}

	t.Run("each worn derived stat adds in its unit: rates, crit damage and penetration in tenths of a percent, regen in tenths of a point", func(t *testing.T) {
		base := data.StartClass().BaseStats
		worn := progression.ItemStats{
			PhysicalAttack: 10, MagicAttack: 4, PhysicalDefense: 3, MagicDefense: 2, MaxHP: 30, MaxMP: 7, Accuracy: 4, Evasion: 2,
			CritRate: 15, CritDamage: 50, Penetration: 120, HPRegen: 5, MPRegen: 3, CooldownSpeed: 10, MoveSpeed: 30,
		}
		bare := progression.DeriveStats(data.StatRules(), kernel.Class("warrior"), 1, base, progression.ItemStats{})
		want := bare
		want.MaxHP, want.MaxMP = bare.MaxHP+30, bare.MaxMP+7
		want.PhysicalAttack, want.MagicAttack, want.PhysicalDefense, want.MagicDefense = bare.PhysicalAttack+10, bare.MagicAttack+4, bare.PhysicalDefense+3, bare.MagicDefense+2
		want.Accuracy, want.Evasion = bare.Accuracy+4, bare.Evasion+2
		want.CritRate, want.CritDamage, want.Penetration = bare.CritRate+0.015, bare.CritDamage+0.05, 0.12
		want.HPRegen, want.MPRegen = bare.HPRegen+0.5, bare.MPRegen+0.3
		want.CooldownSpeed, want.MoveSpeed = bare.CooldownSpeed+0.01, bare.MoveSpeed+0.03
		assertDerived(t, progression.DeriveStats(data.StatRules(), kernel.Class("warrior"), 1, base, worn), want)
	})

	t.Run("worn primary stats count as own ones, toward the curves too", func(t *testing.T) {
		base := data.StartClass().BaseStats
		got := progression.DeriveStats(data.StatRules(), kernel.Class("bowman"), 1, base, progression.ItemStats{STR: 4, DEX: 6, AGI: 9})
		want := progression.DeriveStats(data.StatRules(), kernel.Class("bowman"), 1, progression.AddStats(base, gamedata.Stats{STR: 4, DEX: 6, AGI: 9}), progression.ItemStats{})
		assertDerived(t, got, want)
		if got.MoveSpeed != 0.3*16.0/216 {
			t.Fatalf("move speed %v; worn AGI should move the curve", got.MoveSpeed)
		}
	})

	t.Run("a rate with worn bonuses stops at its limit", func(t *testing.T) {
		got := progression.DeriveStats(data.StatRules(), kernel.Class("bowman"), 1, gamedata.Stats{DEX: 900, AGI: 900, INT: 900}, progression.ItemStats{CritRate: 500, CooldownSpeed: 500, MoveSpeed: 500})
		if got.CritRate != 0.6 || got.CooldownSpeed != 0.4 || got.MoveSpeed != 0.3 {
			t.Fatalf("rates %v, %v, %v; want their limits 0.6, 0.4, 0.3", got.CritRate, got.CooldownSpeed, got.MoveSpeed)
		}
	})

	t.Run("penetration comes from gear alone, and stops at 50%", func(t *testing.T) {
		if got := progression.DeriveStats(data.StatRules(), kernel.Class("warrior"), 30, gamedata.Stats{STR: 500, DEX: 500, INT: 500, AGI: 500, VIT: 500}, progression.ItemStats{}); got.Penetration != 0 {
			t.Fatalf("penetration %v from points", got.Penetration)
		}
		if got := progression.DeriveStats(data.StatRules(), kernel.Class("warrior"), 1, gamedata.Stats{}, progression.ItemStats{Penetration: 900}); got.Penetration != 0.5 {
			t.Fatalf("penetration %v, want the 0.5 limit", got.Penetration)
		}
	})

	t.Run("a class with no base for a stat starts it at zero, and a class the rules don't know has no bases", func(t *testing.T) {
		if got := progression.DeriveStats(data.StatRules(), kernel.Class("warrior"), 1, gamedata.Stats{}, progression.ItemStats{}); got.PhysicalAttack != 0 || got.MaxHP != 60 {
			t.Fatalf("a warrior with no stats: attack %v, max HP %d; want 0 and the base 60", got.PhysicalAttack, got.MaxHP)
		}
		if got := progression.DeriveStats(data.StatRules(), kernel.Class("paladin"), 5, gamedata.Stats{}, progression.ItemStats{}); got.MaxHP != 0 || got.CritRate != 0.05 {
			t.Fatalf("a paladin: max HP %d, crit rate %v; want 0 and the curve's base", got.MaxHP, got.CritRate)
		}
	})
}

func assertDerived(t testing.TB, got, want progression.Derived) {
	t.Helper()
	if got.MaxHP != want.MaxHP || got.MaxMP != want.MaxMP {
		t.Fatalf("HP/MP %d/%d, want %d/%d", got.MaxHP, got.MaxMP, want.MaxHP, want.MaxMP)
	}
	for _, f := range rated(got, want) {
		if math.Abs(f.got-f.want) > 1e-9 {
			t.Fatalf("%s = %v, want %v", f.name, f.got, f.want)
		}
	}
}

type pair struct {
	name      string
	got, want float64
}

// rated pairs every derived stat of the two.
func rated(got, want progression.Derived) []pair {
	return []pair{
		{"max HP", float64(got.MaxHP), float64(want.MaxHP)}, {"max MP", float64(got.MaxMP), float64(want.MaxMP)},
		{"physical attack", got.PhysicalAttack, want.PhysicalAttack}, {"magic attack", got.MagicAttack, want.MagicAttack},
		{"physical defense", got.PhysicalDefense, want.PhysicalDefense}, {"magic defense", got.MagicDefense, want.MagicDefense},
		{"accuracy", got.Accuracy, want.Accuracy}, {"evasion", got.Evasion, want.Evasion},
		{"crit rate", got.CritRate, want.CritRate}, {"crit damage", got.CritDamage, want.CritDamage}, {"penetration", got.Penetration, want.Penetration},
		{"HP regen", got.HPRegen, want.HPRegen}, {"MP regen", got.MPRegen, want.MPRegen},
		{"cooldown speed", got.CooldownSpeed, want.CooldownSpeed}, {"move speed", got.MoveSpeed, want.MoveSpeed},
	}
}

func TestAwardExp(t *testing.T) {
	data := gamedatatest.Shipped(t) // exp_to_next: 1→20, 2→65, 3→129, 4→211 ... cap 30
	tests := []struct {
		name       string
		start      progression.Progress
		amount     int64
		wantAfter  progression.Progress
		wantLevels int
	}{
		{"below the threshold accumulates", progression.Progress{Level: 1, Exp: 5}, 10, progression.Progress{Level: 1, Exp: 15}, 0},
		{"exactly at the threshold levels up with 0 carried", progression.Progress{Level: 1, Exp: 5}, 15, progression.Progress{Level: 2, Exp: 0}, 1},
		{"extra EXP carries over", progression.Progress{Level: 1, Exp: 0}, 30, progression.Progress{Level: 2, Exp: 10}, 1},
		{"several levels at once", progression.Progress{Level: 1, Exp: 0}, 20 + 65 + 129 + 7, progression.Progress{Level: 4, Exp: 7}, 3},
		{"reaching the cap discards the remainder", progression.Progress{Level: 29, Exp: 6000}, 1000, progression.Progress{Level: 30, Exp: 0}, 1},
		{"at the cap nothing accumulates", progression.Progress{Level: 30, Exp: 0}, 5000, progression.Progress{Level: 30, Exp: 0}, 0},
		{"zero EXP changes nothing", progression.Progress{Level: 3, Exp: 50}, 0, progression.Progress{Level: 3, Exp: 50}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := progression.AwardExp(data, tt.start, tt.amount)
			if got.Progress != tt.wantAfter || got.LevelsGained != tt.wantLevels {
				t.Fatalf("got %+v (+%d), want %+v (+%d)", got.Progress, got.LevelsGained, tt.wantAfter, tt.wantLevels)
			}
		})
	}
}

func TestDeathPenalty(t *testing.T) {
	data := gamedatatest.Shipped(t) // level 5 needs 309: 5% is 15.45, floored to 15
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

func drawStats(t *rapid.T, upTo int, label string) gamedata.Stats {
	stat := rapid.IntRange(0, upTo)
	return gamedata.Stats{STR: stat.Draw(t, label+" str"), DEX: stat.Draw(t, label+" dex"), INT: stat.Draw(t, label+" int"), AGI: stat.Draw(t, label+" agi"), VIT: stat.Draw(t, label+" vit")}
}

func drawWorn(t *rapid.T, upTo int) progression.ItemStats {
	b := rapid.IntRange(0, upTo)
	return progression.ItemStats{
		STR: b.Draw(t, "w str"), DEX: b.Draw(t, "w dex"), INT: b.Draw(t, "w int"), AGI: b.Draw(t, "w agi"), VIT: b.Draw(t, "w vit"),
		PhysicalAttack: b.Draw(t, "physical attack"), MagicAttack: b.Draw(t, "magic attack"), PhysicalDefense: b.Draw(t, "physical defense"), MagicDefense: b.Draw(t, "magic defense"),
		MaxHP: b.Draw(t, "max hp"), MaxMP: b.Draw(t, "max mp"), Accuracy: b.Draw(t, "accuracy"), Evasion: b.Draw(t, "evasion"),
		CritRate: b.Draw(t, "crit rate"), CritDamage: b.Draw(t, "crit damage"), Penetration: b.Draw(t, "penetration"), HPRegen: b.Draw(t, "hp regen"), MPRegen: b.Draw(t, "mp regen"),
		CooldownSpeed: b.Draw(t, "cooldown speed"), MoveSpeed: b.Draw(t, "move speed"),
	}
}

var classes = []kernel.Class{"beginner", "warrior", "thief", "bowman", "magician"}

// Whatever is worn, no derived stat drops, and the limits hold.
func TestWearingNeverLowersAStat(t *testing.T) {
	data := gamedatatest.Shipped(t)
	r := data.StatRules()
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom(classes).Draw(t, "class")
		own, level, worn := drawStats(t, 200, "own"), rapid.IntRange(1, 30).Draw(t, "level"), drawWorn(t, 500)
		bare := progression.DeriveStats(r, class, level, own, progression.ItemStats{})
		dressed := progression.DeriveStats(r, class, level, own, worn)
		for _, f := range rated(dressed, bare) {
			if f.got < f.want {
				t.Fatalf("%s fell from %v to %v with %+v worn", f.name, f.want, f.got, worn)
			}
		}
		c := r.Curves
		if dressed.CritRate > c.CritRate.Limit || dressed.CooldownSpeed > c.CooldownSpeed.Limit || dressed.MoveSpeed > c.MoveSpeed.Limit || dressed.Penetration > r.Penetration {
			t.Fatalf("a rate past its limit: %+v", dressed)
		}
	})
}

// A point more in any stat never lowers a derived stat.
func TestAPointNeverLowersAStat(t *testing.T) {
	data := gamedatatest.Shipped(t)
	r := data.StatRules()
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom(classes).Draw(t, "class")
		own, level, worn := drawStats(t, 300, "own"), rapid.IntRange(1, 30).Draw(t, "level"), drawWorn(t, 50)
		more := own
		switch rapid.IntRange(0, 4).Draw(t, "stat") {
		case 0:
			more.STR++
		case 1:
			more.DEX++
		case 2:
			more.INT++
		case 3:
			more.AGI++
		default:
			more.VIT++
		}
		before, after := progression.DeriveStats(r, class, level, own, worn), progression.DeriveStats(r, class, level, more, worn)
		for _, f := range rated(after, before) {
			if f.got < f.want {
				t.Fatalf("%s fell from %v to %v from %+v to %+v", f.name, f.want, f.got, own, more)
			}
		}
	})
}

// Points alone bring a rate ever closer to its limit, never to it.
func TestPointsAloneNeverReachALimit(t *testing.T) {
	data := gamedatatest.Shipped(t)
	r := data.StatRules()
	rapid.Check(t, func(t *rapid.T) {
		class := rapid.SampledFrom(classes).Draw(t, "class")
		got := progression.DeriveStats(r, class, rapid.IntRange(1, 30).Draw(t, "level"), drawStats(t, 100000, "own"), progression.ItemStats{})
		c := r.Curves
		if got.CritRate >= c.CritRate.Limit || got.CooldownSpeed >= c.CooldownSpeed.Limit || got.MoveSpeed >= c.MoveSpeed.Limit {
			t.Fatalf("a rate at its limit from points alone: %+v", got)
		}
	})
}
