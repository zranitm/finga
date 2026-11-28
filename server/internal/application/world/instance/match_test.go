package instance_test

import (
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"sync"
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
	p.Back = &model.Return{MapID: worldtest.Town(data).ID, Player: p}
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
		if len(mv) != 1 || mv[0].Dest.ID != worldtest.Town(data).ID || mv[0].Player.Level != 5 || mv[0].Player.HP != 150 || mv[0].Player.Back != nil {
			t.Fatalf("hand-offs %+v, want Ayla back in town as she was", mv)
		}
		if _, running := mv[0].Player.Timers.Cooldowns["war_cry"]; !running {
			t.Fatal("Ayla's cooldown from before the match is gone")
		}
		for _, cp := range cps.Drain() {
			if cp.ID == bot.ID || cp.MapID != worldtest.Town(data).ID || cp.Level != 5 {
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
		if mv := moves.All(); len(mv) != 1 || mv[0].Dest.ID != worldtest.Town(data).ID {
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
		if cp := saved[len(saved)-1]; cp.MapID != worldtest.Town(data).ID || cp.Level != 5 || cp.HP != 150 || cp.Timers.Cooldowns["war_cry"].IsZero() {
			t.Fatalf("checkpoint %+v, want Ayla as she was in town", cp)
		}
	})
}

// twoFighters runs a duel between Ayla and Robin to its first round.
func twoFighters(t *testing.T, data *gamedata.Data, results *worldtest.Results) (inst *instance.Instance, ayla, robin model.Player, as, rs *worldtest.Sink, stop func()) {
	t.Helper()
	return twoFightersOf(t, data, results, "warrior")
}

// twoFightersOf is twoFighters with Ayla of class.
func twoFightersOf(t *testing.T, data *gamedata.Data, results *worldtest.Results, class kernel.Class) (inst *instance.Instance, ayla, robin model.Player, as, rs *worldtest.Sink, stop func()) {
	t.Helper()
	ayla, robin = fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
	ayla.Class = class
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
		data := pvpData(t, "1.0", "1.0") // attacks alone down a kit within the round
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
		_, _, _, as, _, stop := twoFighters(t, data, results)
		defer stop()
		sleep(data.Pvp().Modes[0].Rounds.Length)
		st := worldtest.Of[model.MatchState](as)
		if last := st[len(st)-1]; last.Phase != model.PhaseBetween || last.Wins != [2]int{0, 0} {
			t.Fatalf("after an untouched round %+v, want an even round", last)
		}
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
			"a teleport":   inst.Teleport(ctx, ayla.ID, c, "willowmere", "", 0),
		} {
			if !errors.Is(err, model.ErrInMatch) {
				t.Errorf("%s: %v, want ErrInMatch", name, err)
			}
		}
	})
}

// pvpData is the shipped data with pvp.yaml's scale set.
func pvpData(t *testing.T, hp, damage string) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{"pvp.yaml": scaleOf(t, hp, damage)})
}

// scaleOf sets pvp.yaml's scale, whatever the harness tuned it to.
func scaleOf(t *testing.T, hp, damage string) func(string) string {
	return func(s string) string {
		if !scaleLine.MatchString(s) {
			t.Fatal("no scale line in pvp.yaml")
		}
		return scaleLine.ReplaceAllString(s, "scale: {hp: "+hp+", damage: "+damage+"}")
	}
}

// scaleLine is pvp.yaml's scale, whatever the harness tuned it to.
var scaleLine = regexp.MustCompile(`(?m)^scale: \{.*\}$`)

// Two fighters who down each other with blows landing on one tick both go down, whoever comes first in the map's order: an even round.
func TestADoubleKnockoutIsAnEvenRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "100.0") // any blow downs
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, &worldtest.Results{})
		defer stop()
		robinAt, _ := rs.Own(robin.ID)
		walkTo(inst, as, ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
		for range 100 {
			_ = inst.Attack(ayla.ID, 0, model.EntityRef{Player: robin.ID})
			_ = inst.Attack(robin.ID, math.Pi, model.EntityRef{Player: ayla.ID})
			sleep(worldtest.TickInterval)
			if len(worldtest.Of[model.Died](as)) > 0 {
				break
			}
		}
		died := worldtest.Of[model.Died](as)
		if len(died) != 2 || died[0].Tick != died[1].Tick {
			t.Fatalf("Died %+v, want both on one tick", died)
		}
		st := worldtest.Of[model.MatchState](as)
		if last := st[len(st)-1]; last.Phase != model.PhaseBetween || last.Wins != [2]int{0, 0} {
			t.Fatalf("after the double knockout %+v, want an even round", last)
		}
	})
}

// blows is what a Thief's attack, Double Stab and its bleed's first tick did to a Warrior in a match, and the Warrior's max HP there.
type blows struct{ attack, skill, bleed, maxHP int }

// thiefOnWarrior is what Ayla, a Thief, deals Robin on data.
func thiefOnWarrior(t *testing.T, data *gamedata.Data) blows {
	ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
	ayla.Class = "thief"
	inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{})
	defer stop()
	as, rs := &worldtest.Sink{}, &worldtest.Sink{}
	for _, j := range []struct {
		p model.Player
		s *worldtest.Sink
	}{{ayla, as}, {robin, rs}} {
		if err := inst.Join(context.Background(), j.p, j.s); err != nil {
			t.Fatal(err)
		}
	}
	sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
	robinAt, _ := rs.Own(robin.ID)
	walkTo(inst, as, ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
	got := blows{maxHP: robinAt.MaxHP}
	_ = inst.Attack(ayla.ID, 0, model.EntityRef{Player: robin.ID})
	sleep(time.Second)
	if hits := hitsOn(as, robin.ID); len(hits) > 0 {
		got.attack = hits[0].Amount
	}
	seen := len(hitsOn(as, robin.ID))
	if err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "double_stab", Root: "double_stab", Target: model.EntityRef{Player: robin.ID}}, 0); err != nil {
		t.Fatal(err)
	}
	sleep(3 * time.Second)
	for _, h := range hitsOn(as, robin.ID)[seen:] {
		switch {
		case h.Status == "bleed" && got.bleed == 0:
			got.bleed = h.Amount
		case h.Status == "" && got.skill == 0:
			got.skill = h.Amount
		}
	}
	if got.attack == 0 || got.skill == 0 || got.bleed == 0 {
		t.Fatalf("Ayla dealt %+v; want an attack, a stab and a bleed tick", got)
	}
	return got
}

// within1 reports whether each of got's blows is want's times f, within the point each rounds by.
func within1(got, want blows, f float64) bool {
	for _, c := range [][2]int{{got.attack, want.attack}, {got.skill, want.skill}, {got.bleed, want.bleed}} {
		if math.Abs(float64(c[0])-float64(c[1])*f) > 1 {
			return false
		}
	}
	return true
}

// scale.damage scales every way a player hurts another: an attack, a damage skill's blows, and the damage over time it leaves.
func TestScaleDamageScalesEveryBlowAndTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		full, half := thiefOnWarrior(t, pvpData(t, "1.0", "1.0")), thiefOnWarrior(t, pvpData(t, "1.0", "0.5"))
		if !within1(half, full, 0.5) {
			t.Fatalf("at half scale %+v, want half of %+v, within a point", half, full)
		}
	})
}

// A class's own factors scale its kit: the Thief's damage every blow and tick it deals, the Warrior's hp its max HP.
func TestAClassesFactorsScaleItsKit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		factors := func(thiefDamage, warriorHP string) *gamedata.Data {
			thief, warrior := regexp.MustCompile(`(?m)^    thief: \{.*\}$`), regexp.MustCompile(`(?m)^    warrior: \{.*\}$`)
			return worldtest.LoadDataWith(t, map[string]func(string) string{"pvp.yaml": func(s string) string {
				if !thief.MatchString(s) || !warrior.MatchString(s) {
					t.Fatal("no thief or warrior factors in pvp.yaml")
				}
				s = thief.ReplaceAllString(s, "    thief: {hp: 1.0, damage: "+thiefDamage+"}")
				return warrior.ReplaceAllString(s, "    warrior: {hp: "+warriorHP+", damage: 1.0}")
			}})
		}
		once, twice := thiefOnWarrior(t, factors("1.0", "1.0")), thiefOnWarrior(t, factors("2.0", "2.0"))
		if !within1(twice, once, 2) || math.Abs(float64(twice.maxHP)-2*float64(once.maxHP)) > 1 {
			t.Fatalf("at twice the factors %+v, want twice %+v, within a point", twice, once)
		}
	})
}

// A forfeit between rounds loses the match there and then, and the fighter goes back.
func TestAForfeitBetweenRoundsLoses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results, moves := &worldtest.Results{}, &worldtest.Moves{}
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{Results: results, Transfer: moves})
		defer stop()
		as := &worldtest.Sink{}
		for _, p := range []model.Player{ayla, robin} {
			sink := &worldtest.Sink{}
			if p.ID == ayla.ID {
				sink = as
			}
			if err := inst.Join(context.Background(), p, sink); err != nil {
				t.Fatal(err)
			}
		}
		r := data.Pvp().Rounds
		sleep(r.Prep + r.Countdown + data.Pvp().Modes[0].Rounds.Length + worldtest.TickInterval)
		if got := phases(as); got[len(got)-1] != model.PhaseBetween {
			t.Fatalf("phases %v, want between rounds", got)
		}
		if err := inst.Forfeit(context.Background(), robin.ID, robin.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		got := results.All()
		if len(got) != 1 || got[0].Winner != 1 || !got[0].Sides[1][0].Left || got[0].Sides[0][0].Left {
			t.Fatalf("outcomes %+v, want Ayla's, Robin gone", got)
		}
		if mv := moves.All(); len(mv) != 1 || mv[0].Player.ID != robin.ID || mv[0].Dest.ID != worldtest.Town(data).ID {
			t.Fatalf("hand-offs %+v, want Robin back in town", mv)
		}
	})
}

// A match that expected players and saw none closes once prep ends, with no outcome to rate; a match of bots alone runs (TestAMatchOfBotsAloneRunsToItsEndTheSameEachTime).
func TestAMatchThatExpectedPlayersAndSawNoneClosesWithNoOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results := &worldtest.Results{}
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), nil, worldtest.Options{Results: results})
		defer stop()
		sleep(data.Pvp().Rounds.Prep + worldtest.TickInterval)
		if !inst.Ended() {
			t.Fatal("the match is still open")
		}
		if got := results.All(); len(got) != 0 {
			t.Fatalf("outcomes %+v, want none", got)
		}
		if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); err == nil {
			t.Fatal("a late arrival joined a closed match")
		}
	})
}

// A world stopping mid-match checkpoints each fighter where they came from, as they were there.
func TestAShutdownMidMatchSavesTheWayBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), cps, worldtest.Options{})
		defer stop()
		if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); err != nil {
			t.Fatal(err)
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + time.Second)
		cps.Drain()
		stop()
		saved := cps.Drain()
		if len(saved) != 1 {
			t.Fatalf("checkpoints at the stop %+v, want Ayla's alone", saved)
		}
		if cp := saved[0]; cp.ID != ayla.ID || cp.MapID != worldtest.Town(data).ID || cp.Level != 5 || cp.HP != 150 || cp.Timers.Cooldowns["war_cry"].IsZero() {
			t.Fatalf("checkpoint %+v, want Ayla as she was in town", cp)
		}
	})
}

// A round's reset tells each fighter, in order, that they're back at their spot, the match's countdown, and their sheet and timers.
func TestAResetTellsRespawnedThenTheMatchThenTheSheet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		_, ayla, _, as, _, stop := twoFighters(t, data, &worldtest.Results{})
		defer stop()
		before := len(as.All())
		sleep(data.Pvp().Modes[0].Rounds.Length + data.Pvp().Rounds.Between)
		var order []string
		for _, e := range as.All()[before:] {
			switch e := e.(type) {
			case model.Respawned:
				if e.ID == ayla.ID {
					order = append(order, "Respawned")
				}
			case model.MatchState:
				if e.Phase == model.PhaseCountdown {
					order = append(order, "MatchState")
				}
			case model.StatsChanged:
				if e.Timers != nil {
					order = append(order, "StatsChanged")
				}
			}
		}
		if want := []string{"Respawned", "MatchState", "StatsChanged"}; !slices.Equal(order, want) {
			t.Fatalf("at the reset %v, want %v", order, want)
		}
	})
}

// A second match asking for a fighter whose first hand-off waits on an item call is refused; the first goes once the call settles.
func TestASecondMatchForAFighterIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
		defer stop()
		p := atSpawn(data, "Ayla")
		p.Connection = "c1"
		join(t, inst, p)
		equipped := equipAsync(inst, p.ID, "c1")
		tick(1)
		ring, _ := data.Map("ring")
		first, second := make(chan error, 1), make(chan error, 1)
		go func() { first <- inst.ToMatch(context.Background(), p.ID, "m-1", ring.ID, ring.Sides[0][0]) }()
		tick(1)
		go func() { second <- inst.ToMatch(context.Background(), p.ID, "m-2", ring.ID, ring.Sides[0][0]) }()
		tick(1)
		if err := <-second; !errors.Is(err, model.ErrInMatch) {
			t.Fatalf("the second match: %v, want ErrInMatch", err)
		}
		close(eq.gate)
		tick(2)
		if err := <-equipped; err != nil {
			t.Fatal(err)
		}
		if err := <-first; err != nil {
			t.Fatalf("the first match: %v", err)
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.moved) != 1 || len(tr.to) != 1 || tr.to[0] != "m-1" {
			t.Fatalf("hand-offs to %v, want the first match's alone", tr.to)
		}
	})
}

// A second arrival of a fighter already in the match is refused, and the first keeps their place.
func TestASecondArrivalKeepsTheFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), nil, worldtest.Options{})
		defer stop()
		first := &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, first); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); !errors.Is(err, model.ErrNotInMatch) {
			t.Fatalf("a second arrival: %v, want ErrNotInMatch", err)
		}
		heard := len(first.Deltas())
		sleep(worldtest.TickInterval)
		if own, ok := first.Own(ayla.ID); !ok || len(first.Deltas()) <= heard {
			t.Fatalf("the first arrival stopped hearing the match: %+v", own)
		}
	})
}

// A fighter who hadn't come by prep's end is gone from the match, and can't arrive late while the others fight on.
func TestAFighterGoneAtPrepsEndCantArriveLate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
		spec := model.MatchSpec{ID: kernel.NewMatchID(), Mode: "duel", Sides: [2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}}
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{})
		defer stop()
		for _, p := range []model.Player{ayla, robin} {
			if err := inst.Join(context.Background(), p, &worldtest.Sink{}); err != nil {
				t.Fatal(err)
			}
		}
		sleep(data.Pvp().Rounds.Prep + worldtest.TickInterval)
		if err := inst.Join(context.Background(), bea, &worldtest.Sink{}); !errors.Is(err, model.ErrNotInMatch) {
			t.Fatalf("Bea arriving after prep: %v, want ErrNotInMatch", err)
		}
		if inst.Ended() {
			t.Fatal("the match closed with Ayla and Robin in it")
		}
	})
}

// A player's hit and the hold it puts on reach the target in one Delta, as a mob's do (#288): the hit's own carries the pin the next input steps with.
func TestAPlayersHitAndItsPinShareADelta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		ayla.Class = "bowman"
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{})
		defer stop()
		as, rs := &worldtest.Sink{}, &worldtest.Sink{}
		for _, j := range []struct {
			p model.Player
			s *worldtest.Sink
		}{{ayla, as}, {robin, rs}} {
			if err := inst.Join(context.Background(), j.p, j.s); err != nil {
				t.Fatal(err)
			}
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		robinAt, _ := rs.Own(robin.ID)
		walkTo(inst, as, ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
		if err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "pinning_shot", Root: "pinning_shot", Target: model.EntityRef{Player: robin.ID}}, 0); err != nil {
			t.Fatal(err)
		}
		sleep(2 * time.Second)
		hits := hitsOn(rs, robin.ID)
		if len(hits) == 0 {
			t.Fatal("the shot never landed")
		}
		var got *sim.State
		for _, d := range rs.Deltas() {
			for _, v := range d.Players {
				if d.Tick == hits[0].Tick && v.ID == robin.ID {
					got = &v.State
				}
			}
		}
		// The kit's pinning_shot is at its last stage, Nailing Shot, which pins for 4 s: its ticks from the next, 3 sub-steps each.
		if want := int(4*time.Second/worldtest.TickInterval) * 3; got == nil || got.Held != want {
			t.Fatalf("the hit's Delta at tick %d: %+v; want held %d sub-steps", hits[0].Tick, got, want)
		}
	})
}

// A player's knockdown on another lasts the down pose, as a mob's does: Charge's Trample lays Robin down in the hit's own Delta.
func TestAPlayersKnockdownLastsTheDownPose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		ayla.Skills.Traits = map[string]string{"charge": "trample"}
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{})
		defer stop()
		as, rs := &worldtest.Sink{}, &worldtest.Sink{}
		for _, j := range []struct {
			p model.Player
			s *worldtest.Sink
		}{{ayla, as}, {robin, rs}} {
			if err := inst.Join(context.Background(), j.p, j.s); err != nil {
				t.Fatal(err)
			}
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		robinAt, _ := rs.Own(robin.ID)
		seq := walkTo(inst, as, ayla.ID, 0, robinAt.State.Z-5, 0.2)
		if err := inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "charge", Root: "strike", Target: model.EntityRef{Player: robin.ID}, Way: sim.Way{Z: 1, Length: 5}}, seq); err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, ayla.ID, seq, sim.Input{}, 40) // the dash starts at the input after the press
		hits := hitsOn(rs, robin.ID)
		if len(hits) == 0 {
			t.Fatal("the charge never landed")
		}
		var got *sim.State
		for _, d := range rs.Deltas() {
			for _, v := range d.Players {
				if d.Tick == hits[0].Tick && v.ID == robin.ID {
					got = &v.State
				}
			}
		}
		if want := int(data.MobRules().React.Down/worldtest.TickInterval) * 3; got == nil || got.Held != want {
			t.Fatalf("the hit's Delta at tick %d: %+v; want held %d sub-steps, the down pose", hits[0].Tick, got, want)
		}
	})
}

// No evasion between players: on a roll that MISSes a mob's blow on a player and a player's on a mob (TestADodgedAttackNeitherKnocksBackNorBlinks and the energy test's MISS), a player's blow on another lands. Dodges and blocks are their defence.
func TestABlowBetweenPlayersNeverMisses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{NewRoll: worldtest.Rolls(0.999)})
		defer stop()
		as, rs := &worldtest.Sink{}, &worldtest.Sink{}
		for _, j := range []struct {
			p model.Player
			s *worldtest.Sink
		}{{ayla, as}, {robin, rs}} {
			if err := inst.Join(context.Background(), j.p, j.s); err != nil {
				t.Fatal(err)
			}
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		robinAt, _ := rs.Own(robin.ID)
		walkTo(inst, as, ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
		if err := inst.Attack(ayla.ID, 0, model.EntityRef{Player: robin.ID}); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		if hits := hitsOn(rs, robin.ID); len(hits) != 1 || hits[0].Miss || hits[0].Amount == 0 {
			t.Fatalf("Ayla's attack on Robin: %+v, want one that lands", hits)
		}
	})
}

// Each player hears the match once a tick at most, as the tick left it, and only when it changed: two fighters arriving on one tick hear it once, naming both, then once for each phase.
func TestTheMatchIsToldOnceATickAsTheTickLeftIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{})
		defer stop()
		sinks := map[kernel.CharacterID]*worldtest.Sink{ayla.ID: {}, robin.ID: {}}
		var joins sync.WaitGroup
		for _, p := range []model.Player{ayla, robin} {
			joins.Go(func() {
				if err := inst.Join(context.Background(), p, sinks[p.ID]); err != nil {
					t.Error(err)
				}
			})
		}
		joins.Wait()
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + time.Second)
		for id, s := range sinks {
			states := worldtest.Of[model.MatchState](s)
			var phases []model.MatchPhase
			for _, st := range states {
				phases = append(phases, st.Phase)
			}
			if want := []model.MatchPhase{model.PhasePrep, model.PhaseCountdown, model.PhaseFight}; !slices.Equal(phases, want) {
				t.Fatalf("%v heard the match in phases %v: %+v; want %v, once each", id, phases, states, want)
			}
			if first := states[0]; first.Sides[0][0].Name == "" || first.Sides[1][0].Name == "" {
				t.Fatalf("%v first heard the match as %+v; want both fighters named, as both arrived that tick", id, first)
			}
		}
	})
}
