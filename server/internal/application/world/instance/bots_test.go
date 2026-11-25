package instance_test

import (
	"context"
	"errors"
	"maps"
	"math"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
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
			for k := 0; len(results.All()) == 0 && k < 600; k++ { // a second a step, up to ten minutes of match
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
			var err error
			for range 40 { // the bot's stun or knockdown may hold Ayla: she presses once it's over
				me, _ := sink.Own(ayla.ID)
				it, _ := sink.Own(bot.ID)
				aim := float32(math.Atan2(it.State.X-me.State.X, it.State.Z-me.State.Z))
				if err = inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "strike", Root: "strike", Aim: aim, Target: model.EntityRef{Player: bot.ID}}, uint32(1000+k)); !errors.Is(err, model.ErrCantAct) { //nolint:gosec // a few presses
					break
				}
				sleep(worldtest.TickInterval)
			}
			if err != nil {
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

// parryOnly is an expert Thief bot whose bar holds Parry alone, so its one guard blocks in place.
func parryOnly() *model.Bot {
	return &model.Bot{ID: kernel.NewCharacterID(), Name: "Ash", Class: "thief", Tier: "expert", Build: model.Build{Bar: []string{"parry", "", "", "", "", "", "", ""}}}
}

// thiefDuel runs Ayla against bot, the expert tier defending at defend and more edits made, both up for the round, from the fight's start.
func thiefDuel(t *testing.T, bot *model.Bot, defend string, more map[string]func(string) string) (*instance.Instance, *worldtest.Sink, model.Player, func()) {
	t.Helper()
	tier, lasting := edit(t, "{id: expert, mu: 30, reaction: 190ms, defend: 0.7", "{id: expert, mu: 30, reaction: 190ms, defend: "+defend), scaleOf(t, "20.0", "1.0")
	edits := map[string]func(string) string{"pvp.yaml": func(s string) string { return lasting(tier(s)) }}
	maps.Copy(edits, more)
	data := worldtest.LoadDataWith(t, edits)
	ayla := fighterFrom(t, data, "Ayla")
	inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: bot}), nil, worldtest.Options{})
	sink := &worldtest.Sink{}
	if err := inst.Join(context.Background(), ayla, sink); err != nil {
		t.Fatal(err)
	}
	sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
	return inst, sink, ayla, stop
}

// xAt presses Ayla's attack at the bot; one pressed during an attack follows at its end, as a held key does.
func xAt(t *testing.T, inst *instance.Instance, sink *worldtest.Sink, ayla model.Player, bot *model.Bot) {
	t.Helper()
	me, _ := sink.Own(ayla.ID)
	it, _ := sink.Own(bot.ID)
	if err := inst.Attack(ayla.ID, float32(math.Atan2(it.State.X-me.State.X, it.State.Z-me.State.Z)), model.EntityRef{Player: bot.ID}); err != nil {
		t.Fatal(err)
	}
}

// closeIn swings until a blow of Ayla's reaches the bot, which comes to her, then waits out a Parry's cooldown; it returns how many swings it took.
func closeIn(t *testing.T, inst *instance.Instance, sink *worldtest.Sink, ayla model.Player, bot *model.Bot) int {
	t.Helper()
	for range 400 {
		if len(blowsOn(sink, ayla.ID, bot.ID)) > 0 {
			sleep(5 * time.Second)
			return len(swingsOf(sink, ayla.ID))
		}
		xAt(t, inst, sink, ayla, bot)
		sleep(worldtest.TickInterval)
	}
	t.Fatal("no swing of Ayla's reached the bot in 20 s")
	return 0
}

// swingsOf are the ticks Ayla's attacks started.
func swingsOf(sink *worldtest.Sink, id kernel.CharacterID) []uint64 {
	var out []uint64
	for _, a := range attacksOf(sink, id) {
		out = append(out, a.Tick)
	}
	return out
}

// attacksOf are id's attacks as the world showed them, each with its length in ticks.
func attacksOf(sink *worldtest.Sink, id kernel.CharacterID) []model.AttackUsed {
	var out []model.AttackUsed
	for _, a := range worldtest.Of[model.AttackUsed](sink) {
		if a.Attacker == id {
			out = append(out, a)
		}
	}
	return out
}

// blowsOn are Ayla's hits on the bot by tick, true where its block stopped them; damage over time left out.
func blowsOn(sink *worldtest.Sink, from, to kernel.CharacterID) map[uint64]bool {
	out := map[uint64]bool{}
	for _, d := range worldtest.Of[model.Damage](sink) {
		if d.Source.Player == from && d.Target.Player == to && d.Status == "" {
			out[d.Tick] = d.Block
		}
	}
	return out
}

// parriesOf are the ticks the bot's Parries started.
func parriesOf(sink *worldtest.Sink, id kernel.CharacterID) []uint64 {
	var out []uint64
	for _, u := range worldtest.Of[model.SkillUsed](sink) {
		if u.Caster == id && u.Skill == "parry" {
			out = append(out, u.Tick)
		}
	}
	return out
}

// Option A of the guard's timing: after a basic attack the bot expects the next swing at its end, so plain X spam is guarded about as often as its tier defends, its Parry ready for every swing here.
func TestXSpamIsGuardedAtTheTiersChance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := parryOnly()
		quick := edit(t, "    effect: block\n    level: 10\n    granted: true\n    energy: 0\n    cooldown: 4s", "    effect: block\n    level: 10\n    granted: true\n    energy: 0\n    cooldown: 600ms")
		inst, sink, ayla, stop := thiefDuel(t, bot, "0.7", map[string]func(string) string{"skills.yaml": quick})
		defer stop()
		for len(swingsOf(sink, ayla.ID)) < 61 {
			xAt(t, inst, sink, ayla, bot)
			sleep(worldtest.TickInterval)
		}
		swings := swingsOf(sink, ayla.ID)[1:61] // the first follows no swing the bot saw
		blows, guarded := blowsOn(sink, ayla.ID, bot.ID), 0
		reached := 0
		for _, s := range swings {
			if blocked, ok := blows[s]; ok {
				reached++
				if blocked {
					guarded++
				}
			}
		}
		share := float64(guarded) / float64(reached)
		t.Logf("%d of %d swings reached, %d of them parried: %.2f", reached, len(swings), guarded, share)
		if reached < 50 || share < 0.5 || share > 0.9 {
			t.Fatalf("%d of %d swings parried, want about the expert's 0.7", guarded, reached)
		}
	})
}

// Cutting X into a skill at the cancel point beats the guard: the bot parries the attack's end, which a second X held to repeat meets, while a skill cut in lands before.
func TestXCutIntoASkillLandsThroughTheGuard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := parryOnly()
		inst, sink, ayla, stop := thiefDuel(t, bot, "1", nil)
		defer stop()
		n := closeIn(t, inst, sink, ayla, bot)
		for len(swingsOf(sink, ayla.ID)) < n+2 { // X held: the second swing at the first's end
			xAt(t, inst, sink, ayla, bot)
			sleep(worldtest.TickInterval)
		}
		held := swingsOf(sink, ayla.ID)[n+1]
		sleep(5 * time.Second) // the Parry cools
		first := len(parriesOf(sink, bot.ID))
		xAt(t, inst, sink, ayla, bot)
		sleep(worldtest.TickInterval)
		cut := attacksOf(sink, ayla.ID)[n+2]
		me, _ := sink.Own(ayla.ID)
		it, _ := sink.Own(bot.ID)
		aim := float32(math.Atan2(it.State.X-me.State.X, it.State.Z-me.State.Z))
		if err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "strike", Root: "strike", Aim: aim, Target: model.EntityRef{Player: bot.ID}}, 900); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		blows := blowsOn(sink, ayla.ID, bot.ID)
		if blocked, ok := blows[held]; !ok || !blocked {
			t.Fatalf("the swing held to repeat at %d: blows %v, want it parried", held, blows)
		}
		var strike uint64
		for _, u := range worldtest.Of[model.SkillUsed](sink) {
			if u.Caster == ayla.ID && u.Skill == "strike" {
				strike = u.Tick
			}
		}
		if blocked, ok := blows[strike]; strike == 0 || strike >= cut.Tick+cut.Length || !ok || blocked {
			t.Fatalf("Strike at %d, cut into the swing at %d of %d ticks: blows %v, want it landing before the swing's end", strike, cut.Tick, cut.Length, blows)
		}
		if parries := parriesOf(sink, bot.ID)[first:]; len(parries) == 0 || parries[0] <= strike {
			t.Fatalf("the bot parried at %v, want once after Strike at %d, for the swing's end", parries, strike)
		}
	})
}

// A skill pressed within castSlack before an attack's cancel point is shown to the match's bots as it starts, as to its watchers, and lands there: a bot quick to guard can't parry it, as it can't one pressed on time.
func TestAnEarlyPressGivesTheBotsNoWarning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := parryOnly()
		quick, lasting := edit(t, "{id: expert, mu: 30, reaction: 190ms, defend: 0.7", "{id: expert, mu: 30, reaction: 50ms, defend: 1"), scaleOf(t, "20.0", "1.0")
		data := worldtest.LoadDataWith(t, map[string]func(string) string{"pvp.yaml": func(s string) string { return lasting(quick(s)) }})
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: bot}), nil, worldtest.Options{})
		defer stop()
		sink := &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, sink); err != nil {
			t.Fatal(err)
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		_, cancel := attackTicks(t, data, ayla)
		n := closeIn(t, inst, sink, ayla, bot)
		before := len(parriesOf(sink, bot.ID))
		xAt(t, inst, sink, ayla, bot)
		sleep(worldtest.TickInterval)
		cut := attacksOf(sink, ayla.ID)[n]
		me, _ := sink.Own(ayla.ID)
		it, _ := sink.Own(bot.ID)
		aim := float32(math.Atan2(it.State.X-me.State.X, it.State.Z-me.State.Z))
		if err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "strike", Root: "strike", Aim: aim, Target: model.EntityRef{Player: bot.ID}}, 900); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		pressed, start := sink.Last().Tick, cut.Tick+cancel
		if pressed >= start {
			t.Fatalf("Strike went at %d, want it within the slack before the swing's cancel point at %d", pressed, start)
		}
		sleep(time.Second)
		var shown uint64
		for _, u := range worldtest.Of[model.SkillUsed](sink) {
			if u.Caster == ayla.ID && u.Skill == "strike" {
				shown = u.Tick
			}
		}
		if shown != start {
			t.Fatalf("Strike shown at %d, want as it starts at the swing's cancel point %d", shown, start)
		}
		blows := blowsOn(sink, ayla.ID, bot.ID)
		if parried, landed := blows[start]; !landed || parried {
			t.Fatalf("Strike pressed at %d, starting at %d: blows %v, want it landing there unparried", pressed, start, blows)
		}
		for _, at := range parriesOf(sink, bot.ID)[before:] {
			if at <= start {
				t.Fatalf("the bot parried at %d, by Strike's start at %d: it saw the press early", at, start)
			}
		}
	})
}

// An X held back past the attack's end lands: the bot's Parry spent on the end it expected, and cooling.
func TestXDelayedPastTheEndLandsWhileTheGuardCools(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := parryOnly()
		inst, sink, ayla, stop := thiefDuel(t, bot, "1", nil)
		defer stop()
		n := closeIn(t, inst, sink, ayla, bot)
		before := len(parriesOf(sink, bot.ID))
		xAt(t, inst, sink, ayla, bot)
		sleep(worldtest.TickInterval)
		first := attacksOf(sink, ayla.ID)[n]
		sleep(time.Duration(first.Length+10) * worldtest.TickInterval) // past the end, and the Parry's 8 ticks over it
		xAt(t, inst, sink, ayla, bot)
		sleep(worldtest.TickInterval)
		late := swingsOf(sink, ayla.ID)[n+1]
		parries := parriesOf(sink, bot.ID)[before:]
		if end := first.Tick + first.Length; len(parries) != 1 || parries[0] <= first.Tick || parries[0] > end {
			t.Fatalf("parried at %v, want once over the first swing's end at %d", parries, end)
		}
		if blows := blowsOn(sink, ayla.ID, bot.ID); !reflect.DeepEqual(blows[late], false) {
			t.Fatalf("the swing at %d held past the end: blows %v, want it landing", late, blows)
		} else if _, ok := blows[late]; !ok {
			t.Fatalf("the swing at %d reached nothing: blows %v", late, blows)
		}
	})
}
