package instance_test

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

func chooseAsync(inst *instance.Instance, id kernel.CharacterID, skill, trait string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.ChooseTrait(context.Background(), id, "c1", skill, trait) }()
	return done
}

// choose sets the skill's trait on the next tick and returns its answer.
func choose(inst *instance.Instance, id kernel.CharacterID, skill, trait string) error {
	done := chooseAsync(inst, id, skill, trait)
	tick(1)
	return <-done
}

// traitSheets are the sheets the player was told with skill on trait.
func traitSheets(s *worldtest.Sink, skill, trait string) []model.StatsChanged {
	var out []model.StatsChanged
	for _, c := range sheets(s) {
		if c.Sheet.Skills.Traits[skill] == trait {
			out = append(out, c)
		}
	}
	return out
}

// C20 §4: a choice is refused for a skill without points or one the class can't learn, then for a trait the skill doesn't have, then for the extra short of the last stage; a refusal changes nothing.
func TestAChoiceOfTraitIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, skillData(t), arena(t, time.Hour), cps, worldtest.Options{CheckpointInterval: time.Hour})
		defer stop()
		tick(1)
		p := caster(spawnZ)
		p.Skills.Points["strike"], p.Skills.Points["haste"] = 24, 1
		s := join(t, inst, p)
		cps.Drain()
		for _, c := range []struct {
			name, skill, trait string
			want               error
		}{
			{"a skill the data doesn't have", "fireball", "ember", model.ErrSkillNotLearned},
			{"a skill without points", "throw", "bola", model.ErrSkillNotLearned},
			{"a skill the class can't learn", "haste", "blur", model.ErrSkillNotLearned},
			{"a trait the skill doesn't have", "strike", "bola", model.ErrTraitUnknown},
			{"a passive's", "second_wind", "swift", model.ErrTraitUnknown},
			{"the granted break-free's", "unyielding", "swift", model.ErrTraitUnknown},
			{"the extra a point short of the last stage", "strike", "concussion", model.ErrTraitLocked},
		} {
			if err := choose(inst, p.ID, c.skill, c.trait); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.name, err, c.want)
			}
		}
		if len(sheets(s)) != 0 || len(cps.Drain()) != 0 {
			t.Fatal("a refusal told a sheet or saved")
		}
		if err := choose(inst, p.ID, "strike", "swift"); err != nil {
			t.Fatalf("the default short of the last stage: %v, want it let through as already chosen", err)
		}
	})
}

// C20 §4: a dead player's choice is refused before anything else is judged.
func TestADeadPlayerChoosesNoTrait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, skillData(t), arena(t, time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
		p.HP = 1
		s := join(t, inst, p)
		untilDead(t, s, p.ID)
		if err := choose(inst, p.ID, "fireball", "ember"); !errors.Is(err, model.ErrCharacterDead) {
			t.Fatalf("dead: %v, want ErrCharacterDead", err)
		}
	})
}

// C20 §4: a choice sets the trait, tells the sheet and saves at once; the trait already chosen answers OK and changes nothing; the default is kept as none.
func TestChoosingATraitSetsItAndSaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, skillData(t), arena(t, time.Hour), cps, worldtest.Options{CheckpointInterval: time.Hour})
		defer stop()
		tick(1)
		p := caster(spawnZ)
		p.Skills.Points["strike"] = 25
		s := join(t, inst, p)
		cps.Drain()
		if err := choose(inst, p.ID, "strike", "concussion"); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"strike": "concussion"}
		if changed := sheets(s); len(changed) != 1 || !maps.Equal(changed[0].Sheet.Skills.Traits, want) {
			t.Fatalf("stats changed %+v, want the sheet with Concussion", changed)
		}
		if saved := cps.Drain(); len(saved) != 1 || !maps.Equal(saved[0].Skills.Traits, want) {
			t.Fatalf("checkpoints %+v, want the choice saved at once", saved)
		}
		if err := choose(inst, p.ID, "strike", "concussion"); err != nil {
			t.Fatal(err)
		}
		if len(sheets(s)) != 1 || len(cps.Drain()) != 0 {
			t.Fatal("choosing the trait already chosen told a sheet or saved")
		}
		if err := choose(inst, p.ID, "strike", "swift"); err != nil {
			t.Fatal(err)
		}
		if changed := sheets(s); len(changed) != 2 || len(changed[1].Sheet.Skills.Traits) != 0 || changed[0].Sheet.Skills.Traits["strike"] != "concussion" {
			t.Fatalf("stats changed %+v, want the default kept as none, the first sheet its own copy", changed)
		}
		if saved := cps.Drain(); len(saved) != 1 || len(saved[0].Skills.Traits) != 0 {
			t.Fatalf("checkpoints %+v, want the default saved", saved)
		}
	})
}

// C20 §4: a fight event, a hit taken or one dealt, keeps a player in combat for 5 s of ticks, counted from the latest; in combat a choice is refused after the trait's own checks.
// Ayla stands by the slime and only takes its hops; Bram throws from 6 m and only kills it.
func TestAFightEventKeepsAPlayerInCombat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillDataWith(t, map[string]func(string) string{"mobs.yaml": edit(t, "hp: 34", "hp: 1")})
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		ayla := caster(nextToSlime)
		ayla.Skills.Points["strike"], ayla.Skills.Points["throw"] = 25, 24
		s := join(t, inst, ayla)
		mobHitsOn(t, s, ayla.ID, 1)
		for _, c := range []struct {
			name, skill, trait string
			want               error
		}{
			{"in combat", "strike", "concussion", model.ErrInCombat},
			{"an unknown trait in combat", "strike", "bola", model.ErrTraitUnknown},
			{"a locked extra in combat", "throw", "bola", model.ErrTraitLocked},
		} {
			if err := choose(inst, ayla.ID, c.skill, c.trait); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.name, err, c.want)
			}
		}
		bram := caster(farFromSlime)
		bram.Name = "Bram"
		bram.Skills.Points["strike"], bram.Skills.Points["throw"] = 25, 25
		sb := join(t, inst, bram)
		if err := cast(inst, bram.ID, "throw"); err != nil {
			t.Fatal(err)
		}

		type side struct {
			name        string
			id          kernel.CharacterID
			sink        *worldtest.Sink
			took, dealt bool
			last        uint64
		}
		sides := []*side{{name: "Ayla", id: ayla.ID, sink: s}, {name: "Bram", id: bram.ID, sink: sb}}
		for _, d := range damages(s) {
			for _, sd := range sides {
				if d.Target.Player == sd.id {
					sd.took, sd.last = true, max(sd.last, d.Tick)
				}
				if d.Source.Player == sd.id {
					sd.dealt, sd.last = true, max(sd.last, d.Tick)
				}
			}
		}
		if a, b := sides[0], sides[1]; !a.took || a.dealt || !b.dealt || b.took {
			t.Fatalf("Ayla took %v, dealt %v; Bram took %v, dealt %v; want Ayla only hit, Bram only hitting", a.took, a.dealt, b.took, b.dealt)
		}
		pending, ok := slices.Clone(sides), map[kernel.CharacterID]bool{}
		for range 200 {
			answers := make([]<-chan error, len(pending))
			for k, sd := range pending {
				answers[k] = chooseAsync(inst, sd.id, "strike", "concussion")
			}
			tick(1)
			var still []*side
			for k, sd := range pending {
				switch err := <-answers[k]; {
				case err == nil:
					ok[sd.id] = true
				case errors.Is(err, model.ErrInCombat):
					still = append(still, sd)
				default:
					t.Fatalf("%s: %v", sd.name, err)
				}
			}
			if pending = still; len(pending) == 0 {
				break
			}
		}
		combat := uint64(data.Combat() / worldtest.TickInterval)
		for _, sd := range sides {
			if !ok[sd.id] {
				t.Fatalf("%s still in combat 10 s on", sd.name)
			}
			if chosen := traitSheets(sd.sink, "strike", "concussion"); len(chosen) != 1 || chosen[0].Tick != sd.last+combat {
				t.Fatalf("%s chose with %+v, want at tick %d: 5 s of ticks after the last fight event at %d", sd.name, chosen, sd.last+combat, sd.last)
			}
		}
	})
}

// C20 §5: a use resolves with its trait: Concussion stuns what Crushing Blow hits and is ready again in 7 s, not 4.5; the map sees the trait.
func TestATraitChangesWhatItsSkillDoes(t *testing.T) {
	for _, trait := range []string{"", "concussion"} {
		t.Run(cmp.Or(trait, "the default"), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := skillDataWith(t, map[string]func(string) string{"mobs.yaml": edit(t, "hp: 34", "hp: 1000000")})
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(nextToSlime)
				p.Skills.Points["strike"] = 25
				if trait != "" {
					p.Skills.Traits = map[string]string{"strike": trait}
				}
				s := join(t, inst, p)
				if err := cast(inst, p.ID, "strike"); err != nil {
					t.Fatal(err)
				}
				if used := eventsOf[model.SkillUsed](s); len(used) != 1 || used[0].Stage != 2 || used[0].Trait != trait {
					t.Fatalf("skills used %+v, want Crushing Blow with %q", used, trait)
				}
				stunned := false
				for _, m := range mobsIn(s) {
					stunned = stunned || slices.ContainsFunc(m.Statuses, func(v model.StatusView) bool { return v.ID == "stun" })
				}
				if stunned != (trait == "concussion") {
					t.Fatalf("the slime stunned %v with %q", stunned, trait)
				}
				tick(int(4500 * time.Millisecond / worldtest.TickInterval))
				err := cast(inst, p.ID, "strike")
				if cooling := errors.Is(err, model.ErrSkillCoolingDown); cooling != (trait == "concussion") || !cooling && err != nil {
					t.Fatalf("Strike again 4.5 s on with %q: %v", trait, err)
				}
			})
		})
	}
}

// C20 §5: a use waiting for its turn keeps the trait it was pressed with, so a change meanwhile can't alter what its client drew.
func TestAWaitingUseKeepsItsTrait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, skillData(t), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(spawnZ)
		p.Skills.Points["strike"] = 25
		p.Skills.Traits = map[string]string{"strike": "concussion"}
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatal(err)
		}
		waiting := castAsync(inst, p.ID, "strike")
		tick(1)
		if err := choose(inst, p.ID, "strike", "swift"); err != nil {
			t.Fatal(err)
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 1 {
			t.Fatalf("skills used %+v, want Strike still waiting for Rally's 1 s to end", used)
		}
		tick(25)
		if err := <-waiting; err != nil {
			t.Fatal(err)
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 2 || used[1].Skill != "strike" || used[1].Trait != "concussion" {
			t.Fatalf("skills used %+v, want Strike with Concussion, as pressed", used)
		}
	})
}

// C20 §5: a buff keeps the trait it was used with: Expose's sheet and its armour break on every hit stay after a change to Precision, and its checkpoint keeps it for a fresh join.
func TestABuffKeepsItsTrait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillDataWith(t, map[string]func(string) string{"mobs.yaml": edit(t, "hp: 34", "hp: 1000000")})
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), cps, worldtest.Options{CheckpointInterval: time.Hour})
		defer stop()
		tick(1)
		p := bowmanAt(farFromSlime)
		p.Connection, p.Energy = "c1", 10
		p.Skills = model.Skills{Points: map[string]int{"focus": 1}, Traits: map[string]string{"focus": "expose"}}
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "focus"); err != nil {
			t.Fatal(err)
		}
		cps.Drain()
		if err := choose(inst, p.ID, "focus", "precision"); err != nil {
			t.Fatal(err)
		}
		changed := sheets(s)
		if last := changed[len(changed)-1]; len(last.Sheet.Skills.Traits) != 0 || last.Sheet.DerivedSkills.CritRate != 0 || last.Sheet.DerivedSkills.Accuracy == 0 {
			t.Fatalf("the sheet after the change %+v, want Focus on Precision and its running buff still Expose's: accuracy, no crit rate", last.Sheet)
		}
		saved := cps.Drain()
		if len(saved) != 1 || saved[0].Timers.Buffs["focus"].Trait != "expose" {
			t.Fatalf("checkpoints %+v, want the buff saved with Expose", saved)
		}
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		broken := false
		for range 20 {
			tick(1)
			for _, m := range mobsIn(s) {
				broken = broken || slices.ContainsFunc(m.Statuses, func(v model.StatusView) bool { return v.ID == "armor_break" })
			}
		}
		if !broken {
			t.Fatal("the shot broke no armour, want Expose's on_hit from the running buff")
		}

		q := bowmanAt(spawnZ)
		q.Connection = "c1"
		q.Skills = model.Skills{Points: map[string]int{"focus": 1}}
		q.Timers = saved[0].Timers
		qs := join(t, inst, q)
		if err := choose(inst, q.ID, "focus", "expose"); err != nil {
			t.Fatal(err)
		}
		if got := sheets(qs); len(got) != 1 || got[0].Sheet.DerivedSkills.CritRate != 0 || got[0].Sheet.DerivedSkills.Accuracy == 0 {
			t.Fatalf("a fresh join's sheet %+v, want the saved buff as Expose's", got)
		}
	})
}

// C20 §4: no fight crosses a map: a player who leaves a fight for the town chooses at once there.
func TestTheTownStartsOutOfCombat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		town, stopTown := worldtest.Run(t, data, nil, worldtest.Options{})
		defer stopTown()
		hunt, stopHunt := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stopHunt()
		tick(1)
		p := caster(nextToSlime)
		p.Skills.Points["strike"] = 25
		s := join(t, hunt, p)
		mobHitsOn(t, s, p.ID, 1)
		if err := choose(hunt, p.ID, "strike", "concussion"); !errors.Is(err, model.ErrInCombat) {
			t.Fatalf("mid-fight: %v, want ErrInCombat", err)
		}
		left := make(chan struct{})
		_ = hunt.Leave(p.ID, "c1", left)
		<-left
		x, z := worldtest.TownSpawn(t, data)
		p.X, p.Z = x, z
		join(t, town, p)
		if err := choose(town, p.ID, "strike", "concussion"); err != nil {
			t.Fatalf("in the town right after the fight: %v", err)
		}
	})
}

// C20 §5: a trait saved before it could open, as a data change can leave one, or one the skill no longer has, is used as the default, and the map sees none.
func TestATraitNotOpenIsUsedAsTheDefault(t *testing.T) {
	for _, trait := range []string{"concussion", "daze"} {
		t.Run(trait, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := skillDataWith(t, map[string]func(string) string{"mobs.yaml": edit(t, "hp: 34", "hp: 1000000")})
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(nextToSlime)
				p.Skills.Points["strike"] = 24
				p.Skills.Traits = map[string]string{"strike": trait}
				s := join(t, inst, p)
				if err := cast(inst, p.ID, "strike"); err != nil {
					t.Fatal(err)
				}
				if used := eventsOf[model.SkillUsed](s); len(used) != 1 || used[0].Stage != 1 || used[0].Trait != "" {
					t.Fatalf("skills used %+v, want Heavy Strike with no trait", used)
				}
				for _, m := range mobsIn(s) {
					if len(m.Statuses) != 0 {
						t.Fatalf("the slime has %+v, want nothing from Concussion", m.Statuses)
					}
				}
			})
		})
	}
}
