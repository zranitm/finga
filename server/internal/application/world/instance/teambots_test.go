package instance_test

import (
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// botAs is a bot of a class at a tier, with its class's first build.
func botAs(data *gamedata.Data, name string, class kernel.Class, tier string) *model.Bot {
	b := botOf(data, name)
	b.Class, b.Tier, b.Build.Bar = class, tier, data.Pvp().Bots.Builds[class][0].Bar
	return b
}

// hpAt is id's HP as s last saw it by tick, -1 before any view.
func hpAt(s *worldtest.Sink, id kernel.CharacterID, tick uint64) int {
	hp := -1
	for _, d := range s.Deltas() {
		if d.Part == nil || d.Tick > tick {
			continue
		}
		for _, v := range d.Players {
			if v.ID == id {
				hp = v.HP
			}
		}
	}
	return hp
}

// A warrior bot's War Cry goes out at the round's start, before any blow reaches, and reaches its teammate, not the enemy.
func TestABotsWarCryAtTheRoundsStartReachesItsTeammate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		bea, robin := fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
		spec := team([2][]model.Fighter{{{Bot: botAs(data, "Ash", "warrior", "novice")}, {Character: bea.ID}}, {{Character: robin.ID}}}, "novice")
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: &worldtest.Results{}})
		defer stop()
		sinks := joinAll(t, inst, bea, robin)
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + 1500*time.Millisecond)
		if !buffedBy(sinks[bea.ID], "war_cry") || buffedBy(sinks[robin.ID], "war_cry") {
			t.Fatalf("War Cry on Bea %v, on Robin %v; want it out at the start, on the teammate and not the enemy", buffedBy(sinks[bea.ID], "war_cry"), buffedBy(sinks[robin.ID], "war_cry"))
		}
	})
}

// A magician bot heals its side once one of them is under 60%, never before, and the heal mends them: here three enemy bots beat on it and two players who stand still.
func TestABotHealsItsSideOnceOneIsUnder60(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		bea, cy := fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Cy")
		healer := botAs(data, "Birch", "magician", "master")
		spec := team([2][]model.Fighter{
			{{Bot: healer}, {Character: bea.ID}, {Character: cy.ID}},
			{{Bot: botAs(data, "Elm", "warrior", "master")}, {Bot: botAs(data, "Fern", "warrior", "master")}, {Bot: botAs(data, "Hazel", "thief", "master")}},
		}, "novice")
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: &worldtest.Results{}})
		defer stop()
		sinks := joinAll(t, inst, bea, cy)
		heals := func() []model.SkillUsed {
			return slices.DeleteFunc(worldtest.Of[model.SkillUsed](sinks[bea.ID]), func(u model.SkillUsed) bool { return u.Caster != healer.ID || u.Skill != "heal" })
		}
		for k := 0; len(heals()) == 0 && k < 600; k++ {
			sleep(100 * time.Millisecond)
		}
		if len(heals()) == 0 {
			t.Fatal("the magician never healed")
		}
		first := heals()[0].Tick
		side := []kernel.CharacterID{healer.ID, bea.ID, cy.ID}
		low := slices.ContainsFunc(side, func(id kernel.CharacterID) bool {
			v, ok := sinks[bea.ID].Own(id)
			hp := hpAt(sinks[bea.ID], id, first-1) // a heal lands as it starts: the tick before
			return ok && hp >= 0 && float64(hp) < 0.6*float64(v.MaxHP)
		})
		if !low {
			t.Fatalf("the first heal at tick %d with none of the side under 60%%", first)
		}
		land := first + uint64(1200*time.Millisecond/worldtest.TickInterval)
		sleep(1200 * time.Millisecond)
		// Nothing else raises HP in a round: HP gained over the heal's second, plus the damage taken in it, comes out above nothing only for a heal.
		mended := slices.ContainsFunc(side, func(id kernel.CharacterID) bool {
			gained := hpAt(sinks[bea.ID], id, land) - hpAt(sinks[bea.ID], id, first-1)
			for _, d := range damages(sinks[bea.ID]) {
				if d.Target.Player == id && d.Tick >= first && d.Tick <= land {
					gained += d.Amount - d.Shielded
				}
			}
			return gained > 0
		})
		if !mended {
			t.Fatal("no one of the side gained HP after the heal")
		}
	})
}

// A 3v3 of bots alone, each side a mix of classes, runs to its end, and from one seed plays the same each time.
func TestATeamMatchOfBotsAloneRunsTheSameEachTime(t *testing.T) {
	data := worldtest.LoadData(t)
	spec := team([2][]model.Fighter{
		{{Bot: botAs(data, "Ash", "warrior", "adept")}, {Bot: botAs(data, "Birch", "magician", "adept")}, {Bot: botAs(data, "Cedar", "bowman", "adept")}},
		{{Bot: botAs(data, "Elm", "thief", "veteran")}, {Bot: botAs(data, "Fern", "warrior", "novice")}, {Bot: botAs(data, "Hazel", "magician", "novice")}},
	}, "adept")
	var runs []model.MatchResult
	for range 2 {
		synctest.Test(t, func(t *testing.T) {
			results := &worldtest.Results{}
			_, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results})
			defer stop()
			for k := 0; len(results.All()) == 0 && k < 900; k++ { // a second a step, up to fifteen minutes of match
				sleep(time.Second)
			}
			got := results.All()
			if len(got) != 1 {
				t.Fatalf("outcomes %+v, want one", got)
			}
			runs = append(runs, got[0])
		})
	}
	r := runs[0]
	if r.Wins[0]+r.Wins[1] < 2 {
		t.Fatalf("outcome %+v, want rounds fought", r)
	}
	if !reflect.DeepEqual(runs[0], runs[1]) {
		t.Fatalf("one seed played %+v, then %+v", runs[0], runs[1])
	}
	t.Logf("adepts against a veteran and two novices: %+v, %v", r.Wins, r.Ended.Sub(r.Started))
}
