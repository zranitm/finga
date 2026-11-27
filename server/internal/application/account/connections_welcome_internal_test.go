package account

import (
	"testing"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
)

// What the world's join found reaches the Welcome whole: the player, its map and tick rate, the waypoints found and the world's view of the character.
func TestAWelcomeKeepsWhatTheJoinFound(t *testing.T) {
	m := copytest.Match{
		Renames: map[string]string{"View": "Joined"},
		Computed: map[string]string{
			"Token":      "the Hello's own, rotated",
			"ServerTime": "the server's clock as the Welcome goes out",
			"Written":    "the pvp module's section, to mark its matches told once sent",
		},
		Skip: map[string]string{"JoinSeq": "the session keeps it, to order the character's joins; the client never sees it"},
	}
	for _, problem := range copytest.Diff(m, func(j model.Joined) model.Welcome {
		var w model.Welcome
		if err := joinedSection(t.Context(), kernel.CharacterID{}, j, &w); err != nil {
			t.Fatal(err)
		}
		return w
	}) {
		t.Error(problem)
	}
}
