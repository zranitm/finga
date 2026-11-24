package gamedata_test

import (
	"maps"
	"slices"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// pvp.md section 6: with no build saved, a character's is their world bar's copy, a block not the class's own swapped for the class's block as a promotion does, and keys the class can't use or repeats left empty; it keeps the traits of the class's active skills, on the bar or not, as a follow-up rides on its root's key.
func TestPvpCopyIsTheWorldBarAsTheClassCanUseIt(t *testing.T) {
	skills := map[string]gamedata.Skill{
		"strike":     {ID: "strike", Kind: gamedata.Active, Class: "beginner", Traits: []gamedata.Trait{{ID: "swift"}}},
		"guard":      {ID: "guard", Kind: gamedata.Active, Effect: gamedata.BlockEffect, Class: "beginner", Granted: true},
		"bastion":    {ID: "bastion", Kind: gamedata.Active, Effect: gamedata.BlockEffect, Class: "warrior", Granted: true},
		"war_cry":    {ID: "war_cry", Kind: gamedata.Active, Class: "warrior", Traits: []gamedata.Trait{{ID: "rally"}}},
		"toughness":  {ID: "toughness", Kind: gamedata.Passive, Class: "warrior"},
		"fire_arrow": {ID: "fire_arrow", Kind: gamedata.Active, Class: "magician"},
		"iron_body":  {ID: "iron_body", Kind: gamedata.Active, Class: "warrior", Traits: []gamedata.Trait{{ID: "steadfast"}}},
	}
	skill := func(id string) (gamedata.Skill, bool) { s, ok := skills[id]; return s, ok }
	warrior := gamedata.Class{ID: "warrior", Line: []kernel.Class{"warrior", "beginner"}, Skills: []string{"war_cry", "toughness", "bastion"}}
	traits := map[string]string{"strike": "swift", "war_cry": "rally", "guard": "steady", "fire_arrow": "blaze", "iron_body": "steadfast"}
	kept := map[string]string{"strike": "swift", "war_cry": "rally", "iron_body": "steadfast"}
	tests := []struct {
		name   string
		bar    []string
		want   []string
		traits map[string]string
	}{
		{"a bar the class can use", []string{"strike", "war_cry", "bastion", "", "", "", "", ""}, []string{"strike", "war_cry", "bastion", "", "", "", "", ""}, kept},
		{"Guard takes the class's block", []string{"guard", "strike", "", "", "", "", "", ""}, []string{"bastion", "strike", "", "", "", "", "", ""}, kept},
		{"the block moves to Guard's key", []string{"strike", "bastion", "guard", "", "", "", "", ""}, []string{"strike", "", "bastion", "", "", "", "", ""}, kept},
		{"another class's skill, a passive, a repeat and an unknown left off", []string{"fire_arrow", "toughness", "strike", "strike", "gone", "war_cry", "", ""}, []string{"", "", "strike", "", "", "war_cry", "", ""}, kept},
		{"a short bar filled out", []string{"war_cry"}, []string{"war_cry", "", "", "", "", "", "", ""}, kept},
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

// pvp.md section 6: a build's keys hold what the client lets a player put there, so a follow-up, which rides on its root's key, is refused on one of its own.
func TestABuildRefusesAFollowUpOnAKey(t *testing.T) {
	skills := map[string]gamedata.Skill{
		"strike": {ID: "strike", Kind: gamedata.Active, Class: "beginner"},
		"charge": {ID: "charge", Kind: gamedata.Active, Class: "warrior"},
	}
	skill := func(id string) (gamedata.Skill, bool) { s, ok := skills[id]; return s, ok }
	warrior := gamedata.Class{ID: "warrior", Line: []kernel.Class{"warrior", "beginner"}, Skills: []string{"charge"}, Chains: []gamedata.Chain{{Root: "strike", Follow: []gamedata.FollowUp{{Skill: "charge"}}}}}
	if err := gamedata.PvpBuildFault(warrior, skill, []string{"strike", "", "", "", "", "", "", ""}, nil); err != nil {
		t.Fatalf("the root alone: %v", err)
	}
	if err := gamedata.PvpBuildFault(warrior, skill, []string{"strike", "", "", "", "", "", "", "charge"}, nil); err == nil {
		t.Fatal("Charge, a follow-up, on a key of its own was taken")
	}
}

// pvp.md section 8: a side's bots keep each class to ⌈team/4⌉ + 1, its players counted: one past even shares of four classes.
func TestAModesClassCap(t *testing.T) {
	for team, want := range map[int]int{1: 2, 3: 2, 4: 2, 5: 3, 15: 5} {
		if got := (gamedata.PvpMode{Team: team}).ClassCap(); got != want {
			t.Errorf("a team of %d: cap %d, want %d", team, got, want)
		}
	}
}
