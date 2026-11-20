package pvp_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	adapter "finga/internal/adapter/pvp"
	"finga/internal/application/character"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// characters is the character module as pvp reads it: its characters by ID, and their skills.
type characters struct {
	of     map[kernel.CharacterID]character.Character
	skills []character.SkillPoints
}

func (c characters) Find(_ context.Context, ids []kernel.CharacterID) ([]character.Character, error) {
	var out []character.Character
	for _, id := range ids {
		if ch, ok := c.of[id]; ok {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (c characters) Skills(context.Context, kernel.CharacterID) ([]character.SkillPoints, error) {
	return c.skills, nil
}

// A character pvp reads is whose they are, their class and world bar, and the traits they chose, a default one left out; none is ErrNoCharacter. Names are those that exist.
func TestACharacterIsReadAsPvpCopiesABuildFromThem(t *testing.T) {
	ayla := character.Character{ID: kernel.NewCharacterID(), AccountID: kernel.NewAccountID(), Name: "Ayla", Class: "warrior", SkillBar: []string{"strike", "", "", "", "", "", "", ""}}
	c := adapter.NewCharacters(characters{
		of:     map[kernel.CharacterID]character.Character{ayla.ID: ayla},
		skills: []character.SkillPoints{{ID: "strike", Points: 25, Trait: "concussion"}, {ID: "war_cry", Points: 5, Trait: ""}},
	})
	got, err := c.Character(context.Background(), ayla.ID)
	if err != nil || got.Account != ayla.AccountID || got.Class != "warrior" || !slices.Equal(got.Bar, ayla.SkillBar) || !maps.Equal(got.Traits, map[string]string{"strike": "concussion"}) {
		t.Fatalf("Ayla read as %+v, %v", got, err)
	}
	if _, err := c.Character(context.Background(), kernel.NewCharacterID()); !errors.Is(err, model.ErrNoCharacter) {
		t.Fatalf("no such character: %v", err)
	}
	names, err := c.Names(context.Background(), []kernel.CharacterID{ayla.ID, kernel.NewCharacterID()})
	if err != nil || len(names) != 1 || names[ayla.ID] != (model.Named{Name: "Ayla", Class: "warrior"}) {
		t.Fatalf("names %+v, %v", names, err)
	}
}
