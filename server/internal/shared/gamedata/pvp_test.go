package gamedata_test

import (
	"maps"
	"slices"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// pvp.md section 6: with no build saved, a character's is their world bar's copy, a block not the class's own swapped for the class's block as a promotion does, and keys the class can't use or repeats left empty; it keeps the traits of the skills on it.
func TestPvpCopyIsTheWorldBarAsTheClassCanUseIt(t *testing.T) {
	skills := map[string]gamedata.Skill{
		"strike":     {ID: "strike", Kind: gamedata.Active, Class: "beginner", Traits: []gamedata.Trait{{ID: "swift"}}},
		"guard":      {ID: "guard", Kind: gamedata.Active, Effect: gamedata.BlockEffect, Class: "beginner", Granted: true},
		"bastion":    {ID: "bastion", Kind: gamedata.Active, Effect: gamedata.BlockEffect, Class: "warrior", Granted: true},
		"war_cry":    {ID: "war_cry", Kind: gamedata.Active, Class: "warrior", Traits: []gamedata.Trait{{ID: "rally"}}},
		"toughness":  {ID: "toughness", Kind: gamedata.Passive, Class: "warrior"},
		"fire_arrow": {ID: "fire_arrow", Kind: gamedata.Active, Class: "magician"},
	}
	skill := func(id string) (gamedata.Skill, bool) { s, ok := skills[id]; return s, ok }
	warrior := gamedata.Class{ID: "warrior", Line: []kernel.Class{"warrior", "beginner"}, Skills: []string{"war_cry", "toughness", "bastion"}}
	traits := map[string]string{"strike": "swift", "war_cry": "rally", "guard": "steady", "fire_arrow": "blaze"}
	tests := []struct {
		name   string
		bar    []string
		want   []string
		traits map[string]string
	}{
		{"a bar the class can use", []string{"strike", "war_cry", "bastion", "", "", "", "", ""}, []string{"strike", "war_cry", "bastion", "", "", "", "", ""}, map[string]string{"strike": "swift", "war_cry": "rally"}},
		{"Guard takes the class's block", []string{"guard", "strike", "", "", "", "", "", ""}, []string{"bastion", "strike", "", "", "", "", "", ""}, map[string]string{"strike": "swift"}},
		{"the block moves to Guard's key", []string{"strike", "bastion", "guard", "", "", "", "", ""}, []string{"strike", "", "bastion", "", "", "", "", ""}, map[string]string{"strike": "swift"}},
		{"another class's skill, a passive, a repeat and an unknown left off", []string{"fire_arrow", "toughness", "strike", "strike", "gone", "war_cry", "", ""}, []string{"", "", "strike", "", "", "war_cry", "", ""}, map[string]string{"strike": "swift", "war_cry": "rally"}},
		{"a short bar filled out", []string{"war_cry"}, []string{"war_cry", "", "", "", "", "", "", ""}, map[string]string{"war_cry": "rally"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar, kept := gamedata.PvpCopy(warrior, skill, tt.bar, traits)
			if !slices.Equal(bar, tt.want) || !maps.Equal(kept, tt.traits) {
				t.Fatalf("copy %v with %v; want %v with %v", bar, kept, tt.want, tt.traits)
			}
			if err := gamedata.PvpBuildFault(warrior, skill, bar, kept); err != nil {
				t.Fatalf("the copy isn't a build: %v", err)
			}
		})
	}
}
