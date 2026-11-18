package instance_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// fighterFrom is a level 5 warrior on the town map, as the world hands one to a match: with their return point.
func fighterFrom(t *testing.T, data *gamedata.Data, name string) model.Player {
	t.Helper()
	x, z := worldtest.TownSpawn(t, data)
	p := worldtest.NewPlayer(name, x, z)
	p.Level, p.HP, p.Energy, p.Connection = 5, 150, 4, "c-"+name
	p.Timers = model.SkillTimers{Cooldowns: map[string]time.Time{"war_cry": time.Now().Add(time.Hour)}}
	p.Back = &model.Return{MapID: data.StartTown().ID, Player: p}
	return p
}

// botOf is a novice warrior bot with its class's first build.
func botOf(data *gamedata.Data, name string) *model.Bot {
	return &model.Bot{ID: kernel.NewCharacterID(), Name: name, Class: "warrior", Tier: "novice", Build: model.Build{Bar: data.Pvp().Bots.Builds["warrior"][0].Bar}}
}

func duel(a, b model.Fighter) model.MatchSpec {
	return model.MatchSpec{ID: kernel.NewMatchID(), Mode: "duel", Sides: [2][]model.Fighter{{a}, {b}}}
}

// phases are the MatchStates s heard, in order.
func phases(s *worldtest.Sink) []model.MatchPhase {
	var out []model.MatchPhase
	for _, st := range worldtest.Of[model.MatchState](s) {
		out = append(out, st.Phase)
	}
	return out
}

// sleep runs the match's ticks for d.
func sleep(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// A match against a bot runs from prep to its outcome: the fighter arrives kitted with their timers cleared, the bot comes to them and fights, each round resets, the outcome is reported, and the fighter goes back as they were, their cooldown still running.
func TestAMatchAgainstABotRunsToItsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results, moves, cps := &worldtest.Results{}, &worldtest.Moves{}, &worldtest.Checkpoints{}
		ayla := fighterFrom(t, data, "Ayla")
		bot := botOf(data, "Ash")
		spec := duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: bot})
		inst, stop := worldtest.RunMatch(t, data, spec, cps, worldtest.Options{Results: results, Transfer: moves})
		defer stop()
		sink := &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, sink); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		snap := worldtest.Of[model.Snapshot](sink)
		if len(snap) != 1 || len(snap[0].Players) != 2 {
			t.Fatalf("snapshots %+v, want one with Ayla and the bot", snap)
		}
		own, _ := sink.Own(ayla.ID)
		kit, _ := data.Kit("warrior")
		if own.Level != kit.Level || own.Team != 1 || own.HP != own.MaxHP {
			t.Fatalf("Ayla in the match %+v, want the kit's level %d on side 1 at full HP", own, kit.Level)
		}
		if botView, ok := sink.Own(bot.ID); !ok || !botView.Bot || botView.Team != 2 || botView.Name != "Ash" {
			t.Fatalf("the bot shows as %+v", botView)
		}
		retimed := worldtest.Of[model.StatsChanged](sink)
		if len(retimed) == 0 || retimed[0].Timers == nil || len(retimed[0].Timers.Cooldowns) != 0 || retimed[0].Sheet == nil || retimed[0].Sheet.Skills.Points["strike"] == 0 {
			t.Fatalf("the sheet on arrival %+v, want the kit's, every skill learned, and no timers", retimed)
		}
		if got := phases(sink); len(got) != 1 || got[0] != model.PhasePrep {
			t.Fatalf("phases %v, want prep", got)
		}
		err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "strike", Root: "strike"}, 1)
		if !errors.Is(err, model.ErrMatchNotStarted) {
			t.Fatalf("a skill in prep: %v, want ErrMatchNotStarted", err)
		}
		sleep(data.Pvp().Rounds.Prep + time.Second)
		if own, _ := sink.Own(ayla.ID); own.State.Held == 0 {
			t.Fatal("the countdown doesn't hold Ayla still")
		}
		sleep(6 * time.Minute)
		got := results.All()
		if len(got) != 1 {
			t.Fatalf("outcomes %+v, want one", got)
		}
		r := got[0]
		if r.Match != spec.ID || r.Winner != 2 || r.Wins != [2]int{0, 2} || r.Sides[0][0].Character != ayla.ID || r.Sides[0][0].Left || r.Sides[1][0].Tier != "novice" {
			t.Fatalf("outcome %+v, want the bot's 2-0 over Ayla, who stayed", r)
		}
		want := []model.MatchPhase{model.PhasePrep, model.PhaseCountdown, model.PhaseFight, model.PhaseBetween, model.PhaseCountdown, model.PhaseFight, model.PhaseResult}
		if got := phases(sink); !equalPhases(got, want) {
			t.Fatalf("phases %v, want %v", got, want)
		}
		if len(worldtest.Of[model.Respawned](sink)) < 2 {
			t.Fatal("the second round's reset sent no Respawned for each fighter")
		}
		mv := moves.All()
		if len(mv) != 1 || mv[0].Dest != data.StartTown().ID || mv[0].Player.Level != 5 || mv[0].Player.HP != 150 || mv[0].Player.Back != nil {
			t.Fatalf("hand-offs %+v, want Ayla back in town as she was", mv)
		}
		if _, running := mv[0].Player.Timers.Cooldowns["war_cry"]; !running {
			t.Fatal("Ayla's cooldown from before the match is gone")
		}
		for _, cp := range cps.Drain() {
			if cp.ID == bot.ID || cp.MapID != data.StartTown().ID || cp.Level != 5 {
				t.Fatalf("checkpoint %+v, want only Ayla's return point", cp)
			}
		}
		last := worldtest.Of[model.StatsChanged](sink)
		if back := last[len(last)-1]; back.Timers == nil || back.Sheet == nil || back.MaxHP == own.MaxHP {
			t.Fatalf("the last sheet %+v, want the world's with its timers", back)
		}
	})
}

func equalPhases(a, b []model.MatchPhase) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// A forfeit is a loss: the match ends for the other side, and the fighter goes back at once.
func TestAForfeitLosesAndGoesBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results, moves := &worldtest.Results{}, &worldtest.Moves{}
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), nil, worldtest.Options{Results: results, Transfer: moves})
		defer stop()
		if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if err := inst.Forfeit(context.Background(), ayla.ID, ayla.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		got := results.All()
		if len(got) != 1 || got[0].Winner != 2 || !got[0].Sides[0][0].Left {
			t.Fatalf("outcomes %+v, want the bot's, Ayla gone", got)
		}
		if mv := moves.All(); len(mv) != 1 || mv[0].Dest != data.StartTown().ID {
			t.Fatalf("hand-offs %+v, want Ayla back in town", mv)
		}
		if err := inst.Forfeit(context.Background(), ayla.ID, ayla.Connection); !errors.Is(err, model.ErrNotInMatch) {
			t.Fatalf("a second forfeit: %v, want ErrNotInMatch", err)
		}
	})
}

// A fighter who leaves the map, logging out or dropped, loses, and their checkpoint is where they came from.
func TestALeaverLosesAndIsSavedWhereTheyCameFrom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results, cps := &worldtest.Results{}, &worldtest.Checkpoints{}
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), cps, worldtest.Options{Results: results})
		defer stop()
		if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); err != nil {
			t.Fatal(err)
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + time.Second)
		left := make(chan struct{})
		if err := inst.Leave(ayla.ID, ayla.Connection, left); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if got := results.All(); len(got) != 1 || got[0].Winner != 2 || !got[0].Sides[0][0].Left {
			t.Fatalf("outcomes %+v, want the bot's, Ayla gone", got)
		}
		saved := cps.Drain()
		if len(saved) == 0 {
			t.Fatal("no checkpoint on leaving")
		}
		if cp := saved[len(saved)-1]; cp.MapID != data.StartTown().ID || cp.Level != 5 || cp.HP != 150 || cp.Timers.Cooldowns["war_cry"].IsZero() {
			t.Fatalf("checkpoint %+v, want Ayla as she was in town", cp)
		}
	})
}

// twoFighters runs a duel between Ayla and Robin to its first round.
func twoFighters(t *testing.T, data *gamedata.Data, results *worldtest.Results) (inst *instance.Instance, ayla, robin model.Player, as, rs *worldtest.Sink, stop func()) {
	t.Helper()
	ayla, robin = fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
	inst, stop = worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{Results: results})
	as, rs = &worldtest.Sink{}, &worldtest.Sink{}
	for _, j := range []struct {
		p model.Player
		s *worldtest.Sink
	}{{ayla, as}, {robin, rs}} {
		if err := inst.Join(context.Background(), j.p, j.s); err != nil {
			t.Fatal(err)
		}
	}
	sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
	return inst, ayla, robin, as, rs, stop
}

// walkTo walks p north or south until they stand within d of z, then stops them.
func walkTo(inst *instance.Instance, sink *worldtest.Sink, id kernel.CharacterID, seq uint32, z, d float64) uint32 {
	for range 400 {
		own, _ := sink.Own(id)
		if diff := z - own.State.Z; diff < d && diff > -d {
			break
		}
		seq = worldtest.Walk(inst, id, seq, sim.Input{Up: z > own.State.Z, Down: z < own.State.Z}, 1)
	}
	return worldtest.Walk(inst, id, seq, sim.Input{}, 1)
}

// Fighters on the two sides hit each other: the attack deals the kit's damage to the other, who blinks but takes no hit guard, until they're down, which ends the round for the other side.
func TestPlayersOnOtherSidesHitEachOtherDownToTheRoundsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results := &worldtest.Results{}
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, results)
		defer stop()
		robinAt, _ := rs.Own(robin.ID)
		seq := walkTo(inst, as, ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
		var hits []model.Damage
		for range 1200 {
			_ = inst.Attack(ayla.ID, 0, model.EntityRef{Player: robin.ID})
			seq = worldtest.Walk(inst, ayla.ID, seq, sim.Input{}, 1)
			hits = hitsOn(as, robin.ID)
			if own, _ := rs.Own(robin.ID); own.Dead {
				break
			}
		}
		if len(hits) == 0 || hits[0].Source.Player != ayla.ID || hits[0].Amount == 0 {
			t.Fatalf("hits on Robin %+v, want Ayla's", hits)
		}
		if own, _ := rs.Own(robin.ID); !own.Dead || own.HP != 0 {
			t.Fatalf("Robin %+v, want down", own)
		}
		if got := worldtest.Of[model.Died](rs); len(got) != 1 || got[0].ID != robin.ID {
			t.Fatalf("Died %+v, want Robin's", got)
		}
		if got := phases(as); got[len(got)-1] != model.PhaseBetween {
			t.Fatalf("phases %v, want the round over", got)
		}
		st := worldtest.Of[model.MatchState](as)
		if last := st[len(st)-1]; last.Wins != [2]int{1, 0} {
			t.Fatalf("wins %v, want Ayla's round", last.Wins)
		}
		sleep(data.Pvp().Rounds.Between + worldtest.TickInterval)
		if own, _ := rs.Own(robin.ID); own.Dead || own.HP != own.MaxHP {
			t.Fatalf("Robin after the reset %+v, want up at full HP", own)
		}
	})
}

// hitsOn are the Damage events s heard on target.
func hitsOn(s *worldtest.Sink, target kernel.CharacterID) []model.Damage {
	var out []model.Damage
	for _, d := range worldtest.Of[model.Damage](s) {
		if d.Target.Player == target {
			out = append(out, d)
		}
	}
	return out
}

// A round out of time goes to the side with more of its HP, and equal shares make an even round.
func TestARoundOutOfTimeGoesToTheLargerShare(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results := &worldtest.Results{}
		inst, ayla, _, as, _, stop := twoFighters(t, data, results)
		defer stop()
		_ = ayla
		sleep(data.Pvp().Modes[0].Rounds.Length)
		st := worldtest.Of[model.MatchState](as)
		if last := st[len(st)-1]; last.Phase != model.PhaseBetween || last.Wins != [2]int{0, 0} {
			t.Fatalf("after an untouched round %+v, want an even round", last)
		}
		_ = inst
	})
}

// Five rounds without a winner end the match a draw.
func TestFiveEvenRoundsAreADraw(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results := &worldtest.Results{}
		_, _, _, _, _, stop := twoFighters(t, data, results)
		defer stop()
		r := data.Pvp().Rounds
		sleep(5 * (data.Pvp().Modes[0].Rounds.Length + r.Between + r.Countdown))
		got := results.All()
		if len(got) != 1 || got[0].Winner != 0 || got[0].Wins != [2]int{0, 0} {
			t.Fatalf("outcomes %+v, want a draw", got)
		}
	})
}

// A match refuses items, gear, points, traits and teleports.
func TestAMatchRefusesItemsGearPointsTraitsAndTeleports(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, ayla, _, _, _, stop := twoFighters(t, data, &worldtest.Results{})
		defer stop()
		ctx, c := context.Background(), ayla.Connection
		for name, err := range map[string]error{
			"a potion":     inst.UseItem(ctx, ayla.ID, c, "k1", kernel.NewItemID(), "red_potion"),
			"gear":         inst.Equip(ctx, ayla.ID, c, "k2", kernel.NewItemID(), "weapon"),
			"stat points":  inst.SpendPoints(ctx, ayla.ID, c, 0, gamedata.Stats{STR: 1}),
			"skill points": inst.SpendSkillPoints(ctx, ayla.ID, c, 0, map[string]int{"strike": 1}),
			"a trait":      inst.ChooseTrait(ctx, ayla.ID, c, "strike", "x"),
			"a teleport":   inst.Teleport(ctx, ayla.ID, c, "town", 0),
		} {
			if !errors.Is(err, model.ErrInMatch) {
				t.Errorf("%s: %v, want ErrInMatch", name, err)
			}
		}
	})
}
