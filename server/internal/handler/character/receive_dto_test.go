package character

import (
	"testing"

	"finga/internal/application/character"
	"finga/pkg/copytest"
)

// A listed or created character reaches the client with every field it shows.
func TestACharacterKeepsWhatTheClientShows(t *testing.T) {
	match := copytest.Match{
		Renames: map[string]string{"OwnStats": "Stats"},
		Skip: map[string]string{
			"AccountID":          "the client lists its own account's characters",
			"Points":             "the client shows the stats, not the points behind them",
			"UnspentPoints":      "points are spent in the game, which the join brings",
			"FreeResetUsed":      "points are reset in the game, which the join brings",
			"UnspentSkillPoints": "skill points are spent in the game, which the join brings",
			"MaxHP":              "the client shows HP alone; the join brings the maximum",
			"Energy":             "energy is the game's, which the join brings",
			"X":                  "the client names the map, not the place on it",
			"Y":                  "the client names the map, not the place on it",
			"Z":                  "the client names the map, not the place on it",
			"Yaw":                "the client names the map, not the way the character faces on it",
			"UpdatedAt":          "the client shows no save times",
		},
	}
	if got := copytest.Diff(match, toCharacter); len(got) > 0 {
		t.Error(got)
	}
	if got := copytest.Diff(match, func(c character.Character) CharacterResponse {
		return toCharacters([]character.Character{c}).Characters[0]
	}); len(got) > 0 {
		t.Errorf("a list: %v", got)
	}
}
