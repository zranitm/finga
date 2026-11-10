package progression_test

import (
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

// Only items whose requirements the own stats meet add anything: a ring's STR never unlocks the sword it falls short for.
func TestActiveSumsTheWornItemsWhoseRequirementsAreMet(t *testing.T) {
	sword := progression.WornItem{Item: gamedata.Item{ID: "knight_sword", Tab: gamedata.Equip, Slot: gamedata.Weapon, Class: kernel.Warrior, MinLevel: 20, Requires: gamedata.Stats{STR: 60}}, Stats: progression.ItemStats{PhysicalAttack: 30, CritRate: 20}}
	ring := progression.WornItem{Item: gamedata.Item{ID: "power_ring", Tab: gamedata.Equip, Slot: "ring"}, Stats: progression.ItemStats{STR: 10}}
	worn := []progression.WornItem{sword, ring}
	if got, want := progression.Active(worn, 20, kernel.Warrior, gamedata.Stats{STR: 60}), (progression.ItemStats{PhysicalAttack: 30, CritRate: 20, STR: 10}); got != want {
		t.Fatalf("meeting the sword's STR: %+v, want both items %+v", got, want)
	}
	if got, want := progression.Active(worn, 20, kernel.Warrior, gamedata.Stats{STR: 55}), (progression.ItemStats{STR: 10}); got != want {
		t.Fatalf("5 STR short, with the ring's 10: %+v, want the ring alone %+v", got, want)
	}
	if got := progression.Active(nil, 1, kernel.Bowman, gamedata.Stats{}); got != (progression.ItemStats{}) {
		t.Fatalf("nothing worn: %+v", got)
	}
}

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
