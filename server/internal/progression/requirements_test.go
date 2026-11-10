package progression_test

import (
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

func TestMeetsChecksLevelClassAndOwnStats(t *testing.T) {
	sword := gamedata.Item{ID: "knight_sword", Tab: gamedata.Equip, Slot: gamedata.Weapon, Class: kernel.Warrior, MinLevel: 20, Requires: gamedata.Stats{STR: 60, DEX: 15}}
	cap := gamedata.Item{ID: "leather_cap", Tab: gamedata.Equip, Slot: "hat", MinLevel: 1}
	strong := gamedata.Stats{STR: 60, DEX: 15}
	tests := []struct {
		name  string
		item  gamedata.Item
		level int
		class kernel.Class
		own   gamedata.Stats
		want  progression.Unmet
	}{
		{"every requirement met, exactly", sword, 20, kernel.Warrior, strong, progression.NothingUnmet},
		{"above every requirement", sword, 25, kernel.Warrior, gamedata.Stats{STR: 90, DEX: 40, VIT: 9}, progression.NothingUnmet},
		{"a level short", sword, 19, kernel.Warrior, strong, progression.LevelUnmet},
		{"another class's weapon", sword, 20, kernel.Bowman, strong, progression.ClassUnmet},
		{"one stat a point short", sword, 20, kernel.Warrior, gamedata.Stats{STR: 60, DEX: 14}, progression.StatsUnmet},
		{"the level counts before the stats", sword, 1, kernel.Warrior, gamedata.Stats{}, progression.LevelUnmet},
		{"armor with no class or stats needed", cap, 1, kernel.Bowman, gamedata.Stats{}, progression.NothingUnmet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progression.Meets(tt.item, tt.level, tt.class, tt.own); got != tt.want {
				t.Fatalf("Meets = %v, want %v", got, tt.want)
			}
		})
	}
}
