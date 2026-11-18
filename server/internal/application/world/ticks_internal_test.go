package world

import (
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
)

// An extra trait's stage is an action too: one shorter than a tick is refused, as a stage's is.
func TestATickLongerThanAnExtraTraitIsRefused(t *testing.T) {
	stage := gamedata.Stage{Name: "Crushing Blow", Length: 900 * time.Millisecond}
	quick := stage
	quick.Length = 20 * time.Millisecond
	skill := gamedata.Skill{ID: "strike", Kind: gamedata.Active, Stages: []gamedata.Stage{stage}, Traits: []gamedata.Trait{{ID: "swift"}, {ID: "jab", Stage: quick}}}
	tick := 50 * time.Millisecond
	if err := actionsLastATick(gamedata.New(gamedata.Parts{Skills: []gamedata.Skill{skill}}), 20, tick); err == nil || !strings.Contains(err.Error(), "strike") {
		t.Fatalf("a 20 ms extra under a 50 ms tick: %v", err)
	}
	skill.Traits[1].Stage = stage
	if err := actionsLastATick(gamedata.New(gamedata.Parts{Skills: []gamedata.Skill{skill}}), 20, tick); err != nil {
		t.Fatalf("a 900 ms extra: %v", err)
	}
}
