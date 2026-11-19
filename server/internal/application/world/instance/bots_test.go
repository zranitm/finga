package instance_test

import (
	"context"
	"math"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
)

// A match of bots alone runs to its end: the rule that closes a match its players never came to holds only for a match that expected players. From one seed, its match and its bots, it plays the same each time.
func TestAMatchOfBotsAloneRunsToItsEndTheSameEachTime(t *testing.T) {
	data := worldtest.LoadData(t)
	thief := botOf(data, "Birch")
	thief.Class, thief.Build.Bar, thief.Tier = "thief", data.Pvp().Bots.Builds["thief"][0].Bar, "master"
	spec := duel(model.Fighter{Bot: botOf(data, "Ash")}, model.Fighter{Bot: thief})
	var runs []model.MatchResult
	for range 2 {
		synctest.Test(t, func(t *testing.T) {
			results := &worldtest.Results{}
			_, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results})
			defer stop()
			for start := time.Now(); len(results.All()) == 0 && time.Since(start) < 10*time.Minute; {
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
	if r.Wins[0]+r.Wins[1] < 2 || r.Winner == 0 {
		t.Fatalf("outcome %+v, want rounds fought to a winner", r)
	}
	if !reflect.DeepEqual(runs[0], runs[1]) {
		t.Fatalf("one seed played %+v, then %+v", runs[0], runs[1])
	}
	t.Logf("the master thief against the novice warrior: %+v, %v", r.Wins, r.Ended.Sub(r.Started))
}

// A bot in a match sees a player's attacks and skills through the match's ring, its reaction late, and guards the blow that may follow, as its brain does on fixed views.
func TestABotGuardsAPlayersAttacksAndSkillsThroughTheRing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sure, lasting := edit(t, "{id: novice, mu: 15, reaction: 450ms, defend: 0.15", "{id: novice, mu: 15, reaction: 100ms, defend: 1"), scaleOf(t, "20.0", "1.0")
		data := worldtest.LoadDataWith(t, map[string]func(string) string{"pvp.yaml": func(s string) string { return lasting(sure(s)) }}) // both up for the round
		ayla := fighterFrom(t, data, "Ayla")
		bot := botOf(data, "Ash")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: bot}), nil, worldtest.Options{})
		defer stop()
		sink := &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, sink); err != nil {
			t.Fatal(err)
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		for range 15 {
			me, _ := sink.Own(ayla.ID)
			it, _ := sink.Own(bot.ID)
			if err := inst.Attack(ayla.ID, float32(math.Atan2(it.State.X-me.State.X, it.State.Z-me.State.Z)), model.EntityRef{Player: bot.ID}); err != nil {
				t.Fatal(err)
			}
			sleep(time.Second)
		}
		guards := func(after uint64) int {
			n := 0
			for _, u := range worldtest.Of[model.SkillUsed](sink) {
				if u.Caster == bot.ID && u.Tick > after && (u.Skill == "bastion" || u.Skill == "roll_back") {
					n++
				}
			}
			return n
		}
		attacks := guards(0)
		if attacks == 0 {
			t.Fatal("the bot never guarded Ayla's attacks")
		}
		var from uint64
		if used := worldtest.Of[model.SkillUsed](sink); len(used) > 0 {
			from = used[len(used)-1].Tick
		}
		for k := range 5 { // Strikes alone now, as the energy pays for, which the ring keeps from the skill's start
			me, _ := sink.Own(ayla.ID)
			it, _ := sink.Own(bot.ID)
			aim := float32(math.Atan2(it.State.X-me.State.X, it.State.Z-me.State.Z))
			if err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "strike", Root: "strike", Aim: aim, Target: model.EntityRef{Player: bot.ID}}, uint32(1000+k)); err != nil { //nolint:gosec // a few presses
				t.Fatalf("Strike %d: %v", k+1, err)
			}
			sleep(4600 * time.Millisecond)
		}
		skills := guards(from)
		if skills == 0 {
			t.Fatal("the bot never guarded Ayla's Strikes")
		}
		t.Logf("%d guards against 15 attacks, %d against 5 Strikes", attacks, skills)
	})
}
