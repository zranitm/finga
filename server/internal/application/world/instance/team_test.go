package instance_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// team is a 3v3 match of these fighters, each side in its spots' order.
func team(sides [2][]model.Fighter, standIn string) model.MatchSpec {
	return model.MatchSpec{ID: kernel.NewMatchID(), Mode: "team", Sides: sides, StandIn: standIn}
}

// joinAll joins each player to inst with a sink of its own.
func joinAll(t *testing.T, inst *instance.Instance, ps ...model.Player) map[kernel.CharacterID]*worldtest.Sink {
	t.Helper()
	sinks := map[kernel.CharacterID]*worldtest.Sink{}
	for _, p := range ps {
		sinks[p.ID] = &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, sinks[p.ID]); err != nil {
			t.Fatal(err)
		}
	}
	return sinks
}

// walkToX walks p east or west until they stand within d of x, then stops them.
func walkToX(inst *instance.Instance, sink *worldtest.Sink, id kernel.CharacterID, seq uint32, x, d float64) uint32 {
	for range 400 {
		own, _ := sink.Own(id)
		if diff := x - own.State.X; diff < d && diff > -d {
			break
		}
		seq = worldtest.Walk(inst, id, seq, sim.Input{Right: x > own.State.X, Left: x < own.State.X}, 1)
	}
	return worldtest.Walk(inst, id, seq, sim.Input{}, 1)
}

// useSkill presses skill as p, aimed along aim, and waits a tick for its answer.
func useSkill(inst *instance.Instance, p model.Player, skill string, aim float32) error {
	done := make(chan error, 1)
	go func() {
		done <- inst.UseSkill(context.Background(), p.ID, p.Connection, model.SkillUse{Skill: skill, Root: skill, Aim: aim}, 0)
	}()
	sleep(worldtest.TickInterval)
	return <-done
}

// buffedBy reports whether the last sheet s heard carries skill's buff.
func buffedBy(s *worldtest.Sink, skill string) bool {
	for _, c := range slices.Backward(worldtest.Of[model.StatsChanged](s)) {
		if sh := c.Sheet; sh != nil {
			for _, b := range sh.Buffs {
				if b.Skill == skill {
					return true
				}
			}
			return false
		}
	}
	return false
}

// In a team match, sides decide who a skill reaches, not parties: an area blow hurts the enemy beside the caster and never the teammate beside them; a heal mends the hurt teammate and never the hurt enemy, though the enemy is in both their parties; and a party buff reaches the teammate and not that enemy.
func TestSidesNotPartiesDecideWhoASkillReaches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, bea, cy := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Cy")
		bea.Class = "magician"
		ayla.Skills.Traits = map[string]string{"slash_blast": "whirlwind"} // a full circle round her
		parties := &fakeParties{parties: [][]kernel.CharacterID{{ayla.ID, bea.ID, cy.ID}}}
		spec := team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: cy.ID}}}, "novice")
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: &worldtest.Results{}, Parties: parties})
		defer stop()
		sinks := joinAll(t, inst, ayla, bea, cy)
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		cyAt, _ := sinks[cy.ID].Own(cy.ID)
		walkTo(inst, sinks[ayla.ID], ayla.ID, 0, cyAt.State.Z-1.2, 0.2)
		seq := walkToX(inst, sinks[bea.ID], bea.ID, 0, cyAt.State.X-1, 0.2)
		walkTo(inst, sinks[bea.ID], bea.ID, seq, cyAt.State.Z-1.2, 0.2)
		for range 200 {
			if own, _ := sinks[ayla.ID].Own(ayla.ID); own.HP < own.MaxHP {
				break
			}
			_ = inst.Attack(cy.ID, math.Pi, model.EntityRef{Player: ayla.ID})
			sleep(worldtest.TickInterval)
		}
		if err := useSkill(inst, ayla, "slash_blast", 0); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		if len(hitsOn(sinks[cy.ID], cy.ID)) == 0 || len(hitsOn(sinks[bea.ID], bea.ID)) != 0 {
			t.Fatalf("hits on Cy %+v, on Bea %+v; want the whirlwind on the enemy and not the teammate beside them", hitsOn(sinks[cy.ID], cy.ID), hitsOn(sinks[bea.ID], bea.ID))
		}
		aylaHurt, _ := sinks[ayla.ID].Own(ayla.ID)
		cyHurt, _ := sinks[cy.ID].Own(cy.ID)
		if aylaHurt.HP >= aylaHurt.MaxHP || cyHurt.HP >= cyHurt.MaxHP {
			t.Fatalf("Ayla %d/%d, Cy %d/%d; want both hurt before the heal", aylaHurt.HP, aylaHurt.MaxHP, cyHurt.HP, cyHurt.MaxHP)
		}
		if err := useSkill(inst, bea, "heal", 0); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		aylaNow, _ := sinks[ayla.ID].Own(ayla.ID)
		cyNow, _ := sinks[cy.ID].Own(cy.ID)
		if aylaNow.HP <= aylaHurt.HP || cyNow.HP > cyHurt.HP {
			t.Fatalf("Ayla %d → %d, Cy %d → %d; want the teammate mended and the enemy not", aylaHurt.HP, aylaNow.HP, cyHurt.HP, cyNow.HP)
		}
		if err := useSkill(inst, ayla, "war_cry", 0); err != nil {
			t.Fatal(err)
		}
		if !buffedBy(sinks[bea.ID], "war_cry") || buffedBy(sinks[cy.ID], "war_cry") {
			t.Fatalf("War Cry on Bea %v, on Cy %v; want the teammate and not the enemy", buffedBy(sinks[bea.ID], "war_cry"), buffedBy(sinks[cy.ID], "war_cry"))
		}
	})
}

// A team fighter who leaves before the outcome, by forfeit, by logging out, or by never coming, while one of their side stays, has a bot of the match's stand-in tier take their body the tick they go: where it stands, with their class, name and HP, the tier's build for the class; the side plays on, the bot fights, a return finds the place taken, and the outcome lists the leaver in their slot as having left.
func TestATeamLeaverIsStoodInTheTickTheyGo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		leave func(t *testing.T, inst *instance.Instance, p model.Player)
	}{
		{"a forfeit", func(t *testing.T, inst *instance.Instance, p model.Player) {
			if err := inst.Forfeit(context.Background(), p.ID, p.Connection); err != nil {
				t.Fatal(err)
			}
		}},
		{"logging out", func(t *testing.T, inst *instance.Instance, p model.Player) {
			done := make(chan struct{})
			if err := inst.Leave(p.ID, p.Connection, done); err != nil {
				t.Fatal(err)
			}
			<-done
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				results, moves := &worldtest.Results{}, &worldtest.Moves{}
				ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
				spec := team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}, "expert") // Bea stays, so her side plays on
				inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results, Transfer: moves})
				defer stop()
				sinks := joinAll(t, inst, ayla, bea, robin)
				sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
				robinAt, _ := sinks[robin.ID].Own(robin.ID)
				walkTo(inst, sinks[ayla.ID], ayla.ID, 0, robinAt.State.Z-1.2, 0.2)
				for range 200 {
					if own, _ := sinks[ayla.ID].Own(ayla.ID); own.HP < own.MaxHP {
						break
					}
					_ = inst.Attack(robin.ID, math.Pi, model.EntityRef{Player: ayla.ID})
					sleep(worldtest.TickInterval)
				}
				sleep(time.Second) // Robin's blow over
				body, _ := sinks[robin.ID].Own(ayla.ID)
				if body.HP >= body.MaxHP {
					t.Fatalf("Ayla %d/%d, want her hurt before she goes", body.HP, body.MaxHP)
				}
				tc.leave(t, inst, ayla)
				sleep(worldtest.TickInterval)
				var standIn model.PlayerView
				var took uint64
				for _, d := range sinks[robin.ID].Deltas() {
					if d.Frame == nil || !slices.Contains(d.Removed, ayla.ID) {
						continue
					}
					took = d.Tick
					for _, v := range d.Players {
						if v.Bot && v.Team == 1 {
							standIn = v
						}
					}
				}
				if took == 0 || standIn.ID.IsZero() {
					t.Fatalf("no frame took Ayla away with a bot in her place")
				}
				// The frame shows the body after the bot's first step: within a tick's walk of where it stood.
				if standIn.Name != "Ayla" || standIn.Class != "warrior" || standIn.HP != body.HP || math.Hypot(standIn.State.X-body.State.X, standIn.State.Z-body.State.Z) > 0.5 {
					t.Fatalf("the stand-in %+v, want Ayla's warrior body where it stood, %+v", standIn, body)
				}
				st := worldtest.Of[model.MatchState](sinks[robin.ID])
				if last := st[len(st)-1]; len(last.Sides[0]) != 2 || last.Sides[0][0].ID != standIn.ID || !last.Sides[0][0].Bot || len(results.All()) != 0 {
					t.Fatalf("the match %+v with outcomes %+v; want it going on, the bot in Ayla's place", last, results.All())
				}
				if err := inst.Join(context.Background(), ayla, &worldtest.Sink{}); !errors.Is(err, model.ErrNotInMatch) {
					t.Fatalf("Ayla coming back: %v, want her place taken", err)
				}
				if mv := moves.All(); len(mv) == 0 && tc.name == "a forfeit" || len(mv) > 0 && mv[0].Dest != data.StartTown().ID {
					t.Fatalf("hand-offs %+v, want Ayla back in town", mv)
				}
				sleep(10 * time.Second)
				if len(hitsBy(sinks[robin.ID], standIn.ID)) == 0 {
					t.Fatal("the stand-in never struck Robin")
				}
				sleep(10 * time.Minute)
				got := results.All()
				if len(got) != 1 || len(got[0].Sides[0]) != 2 || got[0].Sides[0][0].Character != ayla.ID || !got[0].Sides[0][0].Left {
					t.Fatalf("outcomes %+v, want Ayla in her slot, having left", got)
				}
			})
		})
	}
}

// A side whose players have all left concedes at once, as a 1v1 forfeit does: the other side wins, and its last leaver gets no stand-in. So a match whose one player leaves among bots ends then, no bots playing on for nobody.
func TestASideWhoseFightersAllLeaveConcedes(t *testing.T) {
	forfeit := func(t *testing.T, inst *instance.Instance, p model.Player) {
		t.Helper()
		if err := inst.Forfeit(context.Background(), p.ID, p.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
	}
	t.Run("the last of two", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			results := &worldtest.Results{}
			ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
			spec := team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}, "novice")
			inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results})
			defer stop()
			sinks := joinAll(t, inst, ayla, bea, robin)
			sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
			forfeit(t, inst, ayla)
			if got := results.All(); len(got) != 0 {
				t.Fatalf("outcomes %+v with Bea still in; want the match going on", got)
			}
			forfeit(t, inst, bea)
			got := results.All()
			if len(got) != 1 || got[0].Winner != 2 || !got[0].Sides[0][0].Left || got[0].Sides[0][1].Character != bea.ID || !got[0].Sides[0][1].Left {
				t.Fatalf("outcomes %+v; want side 2 winning the tick Bea left, both listed as having left", got)
			}
			for _, st := range worldtest.Of[model.MatchState](sinks[robin.ID]) {
				if st.Sides[0][1].ID != bea.ID {
					t.Fatalf("side 1 %+v; want no stand-in for Bea, the last to leave", st.Sides[0])
				}
			}
		})
	})
	t.Run("one player among bots", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			results := &worldtest.Results{}
			ayla := fighterFrom(t, data, "Ayla")
			spec := team([2][]model.Fighter{
				{{Character: ayla.ID}, {Bot: botOf(data, "Ash")}, {Bot: botOf(data, "Birch")}},
				{{Bot: botOf(data, "Cedar")}, {Bot: botOf(data, "Elm")}, {Bot: botOf(data, "Fern")}},
			}, "novice")
			inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results})
			defer stop()
			joinAll(t, inst, ayla)
			sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
			forfeit(t, inst, ayla)
			if got := results.All(); len(got) != 1 || got[0].Winner != 2 || got[0].Sides[0][0].Character != ayla.ID || !got[0].Sides[0][0].Left {
				t.Fatalf("outcomes %+v; want the bots' side winning the tick Ayla left", got)
			}
		})
	})
}

// A team fighter dropped for a full connection is stood in as one who logs out, and every player hears their side as it now stands, though the drop came as the tick's events were going out.
func TestADroppedTeamFighterIsStoodInForEveryone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
		spec := team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}, "novice")
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{})
		defer stop()
		sinks := joinAll(t, inst, ayla, robin, bea) // Bea last: the others were sent the tick's events before she's dropped
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		sinks[bea.ID].Refuse()
		sleep(3 * worldtest.TickInterval)
		for _, p := range []model.Player{ayla, robin} {
			st := worldtest.Of[model.MatchState](sinks[p.ID])
			if last := st[len(st)-1]; last.Sides[0][1].ID == bea.ID || !last.Sides[0][1].Bot {
				t.Fatalf("%s's last side 1 %+v; want a bot in Bea's place", p.Name, last.Sides[0])
			}
		}
	})
}

// A team fighter who never comes by prep's end is stood in at their spot at full HP, of the class their side has fewest of and a bot's name; the match runs, and they're listed as having left.
func TestATeamFighterWhoNeverCameIsStoodIn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		results := &worldtest.Results{}
		ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
		spec := team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}, "novice")
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results})
		defer stop()
		sinks := joinAll(t, inst, ayla, robin)
		sleep(data.Pvp().Rounds.Prep + worldtest.TickInterval)
		st := worldtest.Of[model.MatchState](sinks[ayla.ID])
		last := st[len(st)-1]
		if len(last.Sides[0]) != 2 || !last.Sides[0][1].Bot || last.Sides[0][1].Class == "warrior" || last.Sides[0][1].Name == "Bea" {
			t.Fatalf("side 1 %+v, want a bot of another class than Ayla's in Bea's place", last.Sides[0])
		}
		v, ok := sinks[ayla.ID].Own(last.Sides[0][1].ID)
		ring, _ := data.Map("ring")
		if !ok || v.HP != v.MaxHP || math.Abs(v.State.X-ring.Sides[0][1].At.X) > 0.5 || math.Abs(v.State.Z-ring.Sides[0][1].At.Z) > 0.5 {
			t.Fatalf("the stand-in %+v, want at Bea's spot %+v at full HP", v, ring.Sides[0][1].At)
		}
		sleep(15 * time.Minute)
		got := results.All()
		if len(got) != 1 || got[0].Sides[0][1].Character != bea.ID || !got[0].Sides[0][1].Left {
			t.Fatalf("outcomes %+v, want Bea in her slot, having left", got)
		}
	})
}
