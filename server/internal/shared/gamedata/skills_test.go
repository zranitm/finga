package gamedata_test

import (
	"testing"

	"finga/internal/shared/gamedata"
)

// C20 §2: the extra opens once the points reach the last stage, a skill of one stage's at its first point; until then, and for any other trait, a use is the stage the points reach.
func TestSpecOpensTheExtraAtTheLastStage(t *testing.T) {
	crushing := gamedata.Stage{At: 25, Name: "Crushing Blow", Targets: 3}
	concussion := crushing
	concussion.Status = gamedata.SkillInflict{Status: "stun"}
	strike := gamedata.Skill{
		Stages: []gamedata.Stage{{At: 0, Name: "Strike"}, {At: 10, Name: "Heavy Strike"}, crushing},
		Traits: []gamedata.Trait{{ID: "swift"}, {ID: "concussion", Stage: concussion}},
	}
	ironBody := gamedata.Stage{At: 0, Name: "Iron Body"}
	ironWall := ironBody
	ironWall.ReachParty = true
	single := gamedata.Skill{
		Stages: []gamedata.Stage{ironBody},
		Traits: []gamedata.Trait{{ID: "steadfast"}, {ID: "iron_wall", Stage: ironWall}},
	}
	tests := []struct {
		name   string
		s      gamedata.Skill
		points int
		trait  string
		open   bool
		want   gamedata.Stage
	}{
		{"the extra a point short of the last stage", strike, 24, "concussion", false, strike.Stages[1]},
		{"the extra at the last stage", strike, 25, "concussion", true, concussion},
		{"the extra past it", strike, 30, "concussion", true, concussion},
		{"the default at the last stage", strike, 25, "swift", true, crushing},
		{"no trait", strike, 25, "", true, crushing},
		{"an unknown trait", strike, 25, "daze", true, crushing},
		{"one stage, no points", single, 0, "iron_wall", false, ironBody},
		{"one stage, its first point", single, 1, "iron_wall", true, ironWall},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.TraitOpen(tt.points); got != tt.open {
				t.Errorf("TraitOpen(%d) = %v, want %v", tt.points, got, tt.open)
			}
			if got := tt.s.Spec(tt.points, tt.trait); got != tt.want {
				t.Errorf("Spec(%d, %q) = %+v, want %+v", tt.points, tt.trait, got, tt.want)
			}
		})
	}
	if i, ok := strike.Trait("concussion"); i != 1 || !ok {
		t.Errorf("Trait(concussion) = %d, %v", i, ok)
	}
	if _, ok := strike.Trait("daze"); ok {
		t.Error("Trait(daze) found")
	}
}
