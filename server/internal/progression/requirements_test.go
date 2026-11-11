package progression_test

import (
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

var (
	beginner = gamedata.Class{ID: "beginner", Line: []kernel.Class{"beginner"}}
	warrior  = gamedata.Class{ID: "warrior", Parent: "beginner", Line: []kernel.Class{"warrior", "beginner"}}
	bowman   = gamedata.Class{ID: "bowman", Parent: "beginner", Line: []kernel.Class{"bowman", "beginner"}}
)

// Only items whose requirements the own stats meet add anything: a ring's STR never unlocks the sword it falls short for.
func TestActiveSumsTheWornItemsWhoseRequirementsAreMet(t *testing.T) {
	sword := progression.WornItem{Item: gamedata.Item{ID: "knight_sword", Tab: gamedata.Equip, Slot: gamedata.Weapon, Class: kernel.Class("warrior"), MinLevel: 20, Requires: gamedata.Stats{STR: 60}}, Stats: progression.ItemStats{PhysicalAttack: 30, CritRate: 20}}
	ring := progression.WornItem{Item: gamedata.Item{ID: "power_ring", Tab: gamedata.Equip, Slot: "ring"}, Stats: progression.ItemStats{STR: 10}}
	worn := []progression.WornItem{sword, ring}
	if got, want := progression.Active(worn, 20, warrior, gamedata.Stats{STR: 60}), (progression.ItemStats{PhysicalAttack: 30, CritRate: 20, STR: 10}); got != want {
		t.Fatalf("meeting the sword's STR: %+v, want both items %+v", got, want)
	}
	if got, want := progression.Active(worn, 20, warrior, gamedata.Stats{STR: 55}), (progression.ItemStats{STR: 10}); got != want {
		t.Fatalf("5 STR short, with the ring's 10: %+v, want the ring alone %+v", got, want)
	}
	if got := progression.Active(nil, 1, bowman, gamedata.Stats{}); got != (progression.ItemStats{}) {
		t.Fatalf("nothing worn: %+v", got)
	}
}

func TestMeetsChecksLevelClassAndOwnStats(t *testing.T) {
	sword := gamedata.Item{ID: "knight_sword", Tab: gamedata.Equip, Slot: gamedata.Weapon, Class: kernel.Class("warrior"), MinLevel: 20, Requires: gamedata.Stats{STR: 60, DEX: 15}}
	cap := gamedata.Item{ID: "leather_cap", Tab: gamedata.Equip, Slot: "hat", MinLevel: 1}
	club := gamedata.Item{ID: "training_club", Tab: gamedata.Equip, Slot: gamedata.Weapon, Class: "beginner", MinLevel: 1}
	strong := gamedata.Stats{STR: 60, DEX: 15}
	tests := []struct {
		name  string
		item  gamedata.Item
		level int
		class gamedata.Class
		own   gamedata.Stats
		want  progression.Unmet
	}{
		{"every requirement met, exactly", sword, 20, warrior, strong, progression.NothingUnmet},
		{"above every requirement", sword, 25, warrior, gamedata.Stats{STR: 90, DEX: 40, VIT: 9}, progression.NothingUnmet},
		{"a level short", sword, 19, warrior, strong, progression.LevelUnmet},
		{"another class's weapon", sword, 20, bowman, strong, progression.ClassUnmet},
		{"DEX a point short", sword, 20, warrior, gamedata.Stats{STR: 60, DEX: 14}, progression.DEXUnmet},
		{"STR a point short", sword, 20, warrior, gamedata.Stats{STR: 59, DEX: 15}, progression.STRUnmet},
		{"both short: STR is named first", sword, 20, warrior, gamedata.Stats{}, progression.STRUnmet},
		{"the level counts before the stats", sword, 1, warrior, gamedata.Stats{}, progression.LevelUnmet},
		{"armor with no class or stats needed", cap, 1, bowman, gamedata.Stats{}, progression.NothingUnmet},
		{"a Beginner's weapon, for a class promoted from it", club, 1, warrior, gamedata.Stats{}, progression.NothingUnmet},
		{"a class's weapon, for a Beginner", sword, 20, beginner, strong, progression.ClassUnmet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progression.Meets(tt.item, tt.level, tt.class, tt.own); got != tt.want {
				t.Fatalf("Meets = %v, want %v", got, tt.want)
			}
		})
	}
}
