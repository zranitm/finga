package progression_test

import (
	"testing"

	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/progression"
)

var sword = gamedata.Rolls{PhysicalAttack: gamedata.Range{Min: 10, Max: 12}, CritRate: gamedata.Range{Min: 5, Max: 15}, MoveSpeed: gamedata.Range{Min: 1, Max: 3}}

func TestTheLowestRollTakesEachMinimum(t *testing.T) {
	if got, want := progression.Lowest(sword), (progression.ItemStats{PhysicalAttack: 10, CritRate: 5, MoveSpeed: 1}); got != want {
		t.Fatalf("lowest %+v, want %+v", got, want)
	}
}

func TestARollFitsItsRanges(t *testing.T) {
	tests := []struct {
		name string
		roll progression.ItemStats
		want bool
	}{
		{"the lowest", progression.ItemStats{PhysicalAttack: 10, CritRate: 5, MoveSpeed: 1}, true},
		{"the highest", progression.ItemStats{PhysicalAttack: 12, CritRate: 15, MoveSpeed: 3}, true},
		{"between", progression.ItemStats{PhysicalAttack: 11, CritRate: 9, MoveSpeed: 2}, true},
		{"one stat below its range", progression.ItemStats{PhysicalAttack: 9, CritRate: 5, MoveSpeed: 1}, false},
		{"the last stat above its range", progression.ItemStats{PhysicalAttack: 10, CritRate: 5, MoveSpeed: 4}, false},
		{"a stat the item doesn't give", progression.ItemStats{PhysicalAttack: 10, CritRate: 5, MoveSpeed: 1, STR: 1}, false},
		{"a new stat the item doesn't give", progression.ItemStats{PhysicalAttack: 10, CritRate: 5, MoveSpeed: 1, Penetration: 1}, false},
		{"nothing at all", progression.ItemStats{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.roll.Fits(sword); got != tt.want {
				t.Fatalf("Fits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRolledPicksEveryStatInOneOrder(t *testing.T) {
	var picked []gamedata.Range
	progression.Rolled(sword, func(r gamedata.Range) int {
		picked = append(picked, r)
		return r.Max
	})
	if len(picked) != 18 || picked[5] != sword.PhysicalAttack || picked[12] != sword.CritRate || picked[17] != sword.MoveSpeed {
		t.Fatalf("picked %v; want all eighteen stats, the five primary first, physical attack sixth, crit rate thirteenth and move speed last", picked)
	}
}

func TestPlusAddsEveryStat(t *testing.T) {
	a := progression.ItemStats{STR: 1, PhysicalAttack: 2, Penetration: 3, MoveSpeed: 4}
	b := progression.ItemStats{STR: 10, MagicDefense: 20, Penetration: 30, MoveSpeed: 40}
	if got, want := a.Plus(b), (progression.ItemStats{STR: 11, PhysicalAttack: 2, MagicDefense: 20, Penetration: 33, MoveSpeed: 44}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEveryPickWithinTheRangesFits(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := func(stat string) gamedata.Range {
			lo := rapid.IntRange(0, 50).Draw(t, stat+" min")
			return gamedata.Range{Min: lo, Max: rapid.IntRange(lo, lo+50).Draw(t, stat+" max")}
		}
		r := gamedata.Rolls{
			STR: in("str"), DEX: in("dex"), INT: in("int"), AGI: in("agi"), VIT: in("vit"),
			PhysicalAttack: in("physical attack"), MagicAttack: in("magic attack"), PhysicalDefense: in("physical defense"), MagicDefense: in("magic defense"),
			MaxHP: in("max hp"), Accuracy: in("accuracy"), Evasion: in("evasion"),
			CritRate: in("crit rate"), CritDamage: in("crit damage"), Penetration: in("penetration"), HPRegen: in("hp regen"),
			CooldownSpeed: in("cooldown speed"), MoveSpeed: in("move speed"),
		}
		roll := progression.Rolled(r, func(in gamedata.Range) int { return rapid.IntRange(in.Min, in.Max).Draw(t, "value") })
		if !roll.Fits(r) {
			t.Fatalf("%+v does not fit %+v", roll, r)
		}
	})
}
