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
	"finga/internal/shared/kernel"
)

// arenaBar is a warrior's arena bar holding the given skills, the rest of its eight keys empty.
func arenaBar(skills ...string) []string {
	return append(skills, make([]string, 8-len(skills))...)
}

// fromKey uses skill from its own key, as a client does.
func fromKey(inst *instance.Instance, p model.Player, skill string) error {
	return inst.UseSkill(context.Background(), p.ID, p.Connection, model.SkillUse{Skill: skill, Root: skill}, 0)
}

// pvp.md section 6: in a match, a skill goes only from a key of its fighter's arena bar, a follow-up from its root's; a player whose build hasn't come is refused nothing. The build sent in prep is worn from the first countdown, its traits too; one sent in a round waits for the next countdown, and an older revision than one sent changes nothing.
func TestAMatchTakesSkillsFromTheArenaBarAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Character: robin.ID}), nil, worldtest.Options{Results: &worldtest.Results{}})
		defer stop()
		as := &worldtest.Sink{}
		for _, j := range []struct {
			p model.Player
			s *worldtest.Sink
		}{{ayla, as}, {robin, &worldtest.Sink{}}} {
			if err := inst.Join(context.Background(), j.p, j.s); err != nil {
				t.Fatal(err)
			}
		}
		ctx := context.Background()
		first := model.Build{Bar: arenaBar("strike", "slash_blast", "war_cry"), Traits: map[string]string{"strike": "concussion"}, Revision: 2}
		if err := inst.SetBuild(ctx, ayla.ID, first); err != nil {
			t.Fatal(err)
		}
		if err := inst.SetBuild(ctx, kernel.NewCharacterID(), first); err != nil {
			t.Fatalf("a build for no fighter of the match: %v", err)
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		uses := []struct {
			who   model.Player
			skill string
			want  error
		}{
			{ayla, "strike", nil},
			{ayla, "taunt", model.ErrNotOnArenaBar},
			{ayla, "war_cry", nil},
			{robin, "taunt", nil}, // no build came: nothing refused
		}
		for _, u := range uses {
			if err := fromKey(inst, u.who, u.skill); !errors.Is(err, u.want) {
				t.Fatalf("%s's %s in round 1: %v, want %v", u.who.Name, u.skill, err, u.want)
			}
			sleep(1500 * time.Millisecond)
		}
		if used := worldtest.Of[model.SkillUsed](as); len(used) == 0 || used[0].Skill != "strike" || used[0].Trait != "concussion" {
			t.Fatalf("Ayla's uses %+v; want her strike with the build's trait", used)
		}
		follow := inst.UseSkill(ctx, ayla.ID, ayla.Connection, model.SkillUse{Skill: "iron_body", Root: "war_cry"}, 0)
		if follow != nil {
			t.Fatalf("Iron Body from War Cry's key, within its 3 s: %v", follow)
		}
		sleep(1500 * time.Millisecond)
		next := model.Build{Bar: arenaBar("taunt"), Traits: map[string]string{}, Revision: 3}
		if err := inst.SetBuild(ctx, ayla.ID, next); err != nil {
			t.Fatal(err)
		}
		if err := inst.SetBuild(ctx, ayla.ID, first); err != nil { // the older, landing late
			t.Fatal(err)
		}
		if err := fromKey(inst, ayla, "taunt"); !errors.Is(err, model.ErrNotOnArenaBar) {
			t.Fatalf("Taunt in the round its build came: %v, want refused till the next countdown", err)
		}
		for phase := phases(as); phase[len(phase)-1] != model.PhaseFight || len(phase) < 5; phase = phases(as) {
			sleep(time.Second)
		}
		if err := fromKey(inst, ayla, "taunt"); err != nil {
			t.Fatalf("Taunt in round 2: %v", err)
		}
		sleep(1500 * time.Millisecond)
		if err := fromKey(inst, ayla, "strike"); !errors.Is(err, model.ErrNotOnArenaBar) {
			t.Fatalf("Strike in round 2, off the new bar: %v", err)
		}
	})
}

// Out of a match no bar is judged: a skill the player has goes from any key.
func TestOutOfAMatchNoBarIsJudged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 20, 20)
		p.Class, p.Level, p.Energy = "warrior", 30, 10
		p.Skills = model.Skills{Points: map[string]int{"taunt": 1}}
		join(t, inst, p)
		if err := fromKey(inst, p, "taunt"); err != nil {
			t.Fatalf("Taunt on a map: %v", err)
		}
	})
}

// A build sent to a fighter who never came is put on no one: the countdown wears the others' and the match plays on.
func TestABuildForAFighterWhoNeverCameIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
		spec := team([2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}, "novice")
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: &worldtest.Results{}})
		defer stop()
		joinAll(t, inst, ayla, robin)
		ctx := context.Background()
		for _, id := range []kernel.CharacterID{ayla.ID, bea.ID} {
			if err := inst.SetBuild(ctx, id, model.Build{Bar: arenaBar("strike"), Traits: map[string]string{}, Revision: 1}); err != nil {
				t.Fatal(err)
			}
		}
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		if err := fromKey(inst, ayla, "taunt"); !errors.Is(err, model.ErrNotOnArenaBar) {
			t.Fatalf("Ayla's Taunt, off her build, with Bea never come: %v", err)
		}
	})
}
