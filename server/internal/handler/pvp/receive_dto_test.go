package pvp

import (
	"testing"

	"finga/internal/application/pvp"
	"finga/pkg/copytest"
)

// A character's standings and arena build reach the client with every field.
func TestAProfileKeepsEveryField(t *testing.T) {
	if got := copytest.Diff(copytest.Match{
		Renames: map[string]string{"Build.Bar": "Build.SkillBar"},
		Skip:    map[string]string{"Build.Revision": "the client reads a build by its keys and traits; its revision is the module's own"},
	}, toPvpProfile); len(got) > 0 {
		t.Error(got)
	}
}

// A board's rows reach the client with every field.
func TestALeaderboardRowKeepsEveryField(t *testing.T) {
	match := copytest.Match{Renames: map[string]string{"Character": "CharacterID"}}
	if got := copytest.Diff(match, func(r pvp.Row) LeaderboardRowResponse { return toLeaderboard("duel", []pvp.Row{r}).Rows[0] }); len(got) > 0 {
		t.Error(got)
	}
}
