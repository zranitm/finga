package session

import (
	"testing"

	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
)

// What the world's join found reaches the Welcome whole: the character, its map and tick rate, quests, timers and waypoints found.
func TestAWelcomeKeepsWhatTheJoinFound(t *testing.T) {
	m := copytest.Match{
		Computed: map[string]string{
			"Token":             "the Hello's own, rotated",
			"InventoryRevision": "the inventory's section, with its items",
			"ServerTime":        "the gateway's clock as the Welcome goes out",
			"Missed.Older":      "the pvp module's section, as its matches",
			"Written":           "the pvp module's section, to mark its matches told once sent",
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
