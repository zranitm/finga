package local

import (
	"context"
	"testing"

	"finga/internal/kernel"
	"finga/modules/character"
	"finga/modules/gateway/internal/model"
	"finga/pkg/copytest"
)

// characterModule answers List and Create with its characters.
type characterModule struct{ list []character.Character }

func (m *characterModule) Create(context.Context, kernel.AccountID, string) (character.Character, error) {
	return m.list[0], nil
}

func (m *characterModule) List(context.Context, kernel.AccountID) ([]character.Character, error) {
	return m.list, nil
}

func (m *characterModule) SetSkillBar(context.Context, kernel.CharacterID, kernel.AccountID, []string) error {
	return nil
}

// A listed or created character reaches the lobby with every field it shows.
func TestACharacterKeepsWhatTheLobbyShows(t *testing.T) {
	m := &characterModule{}
	c := NewCharacters(m)
	match := copytest.Match{
		Renames: map[string]string{"OwnStats": "Stats"},
		Skip: map[string]string{
			"AccountID":          "the lobby lists its own account's characters",
			"Points":             "the lobby shows the stats, not the points behind them",
			"UnspentPoints":      "points are spent in the game, which the join brings",
			"FreeResetUsed":      "points are reset in the game, which the join brings",
			"UnspentSkillPoints": "skill points are spent in the game, which the join brings",
			"MaxHP":              "the lobby shows HP alone; the join brings the maximum",
			"Energy":             "energy is the game's, which the join brings",
			"X":                  "the lobby names the map, not the place on it",
			"Y":                  "the lobby names the map, not the place on it",
			"UpdatedAt":          "the lobby shows no save times",
		},
	}
	for name, call := range map[string]func() model.Character{
		"List": func() model.Character {
			list, err := c.List(t.Context(), kernel.AccountID{})
			if err != nil || len(list) != 1 {
				t.Fatalf("listed %v (%v)", list, err)
			}
			return list[0]
		},
		"Create": func() model.Character {
			out, err := c.Create(t.Context(), kernel.AccountID{}, "")
			if err != nil {
				t.Fatal(err)
			}
			return out
		},
	} {
		got := copytest.Diff(match, func(ch character.Character) model.Character {
			m.list = []character.Character{ch}
			return call()
		})
		if len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}
