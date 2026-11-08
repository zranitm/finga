package progression_test

import (
	"testing"

	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/progression"
)

var sword = gamedata.Rolls{Attack: gamedata.Range{Min: 10, Max: 12}, CritRate: gamedata.Range{Min: 5, Max: 15}}

func TestTheLowestRollTakesEachMinimum(t *testing.T) {
	if got, want := progression.Lowest(sword), (progression.ItemStats{Attack: 10, CritRate: 5}); got != want {
		t.Fatalf("lowest %+v, want %+v", got, want)
	}
}

func TestARollFitsItsRanges(t *testing.T) {
	tests := []struct {
		name string
		roll progression.ItemStats
		want bool
	}{
		{"the lowest", progression.ItemStats{Attack: 10, CritRate: 5}, true},
		{"the highest", progression.ItemStats{Attack: 12, CritRate: 15}, true},
		{"between", progression.ItemStats{Attack: 11, CritRate: 9}, true},
		{"one stat below its range", progression.ItemStats{Attack: 9, CritRate: 5}, false},
		{"one stat above its range", progression.ItemStats{Attack: 10, CritRate: 16}, false},
		{"a stat the item doesn't give", progression.ItemStats{Attack: 10, CritRate: 5, STR: 1}, false},
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
	if len(picked) != 13 || picked[0] != sword.Attack || picked[9] != sword.CritRate {
		t.Fatalf("picked %v; want all thirteen stats, attack first and crit rate tenth", picked)
	}
}

func TestEveryPickWithinTheRangesFits(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := func(stat string) gamedata.Range {
			lo := rapid.IntRange(0, 50).Draw(t, stat+" min")
			return gamedata.Range{Min: lo, Max: rapid.IntRange(lo, lo+50).Draw(t, stat+" max")}
		}
		r := gamedata.Rolls{
			Attack: in("attack"), Defense: in("defense"), STR: in("str"), DEX: in("dex"), INT: in("int"), AGI: in("agi"), VIT: in("vit"),
			Accuracy: in("accuracy"), Evasion: in("evasion"), CritRate: in("crit rate"), CritDamage: in("crit damage"), MaxHP: in("max hp"), MaxMP: in("max mp"),
		}
		roll := progression.Rolled(r, func(in gamedata.Range) int { return rapid.IntRange(in.Min, in.Max).Draw(t, "value") })
		if !roll.Fits(r) {
			t.Fatalf("%+v does not fit %+v", roll, r)
		}
	})
}
