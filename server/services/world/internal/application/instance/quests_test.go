package instance_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/pkg/transient"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/quests"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// questData is the shipped data with two more of the town guide's quests: an errand (talk to the guide, reach level 3) and a repeatable bounty (a slime, two gels; a cap and three potions).
func questData(t *testing.T) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{
		"npcs.yaml": func(s string) string {
			return strings.ReplaceAll(s, "ready_for_the_wilds]", "ready_for_the_wilds, errand, bounty]")
		},
		"quests.yaml": func(s string) string {
			return s + `
  - id: errand
    name: Errand
    giver: town_guide
    turn_in: town_guide
    objectives:
      - {talk: town_guide}
      - {level: 3}
    rewards: {exp: 7}
  - id: bounty
    name: Bounty
    giver: town_guide
    turn_in: town_guide
    repeatable: true
    objectives:
      - {kill: green_slime, count: 1}
      - {collect: slime_gel, count: 2}
    rewards:
      exp: 4
      items: [{item: leather_cap, quantity: 1}, {item: red_potion, quantity: 3}]
`
		},
	})
}

// fakeHandIns records each hand-in and answers with answer or err, once gate (if set) opens.
type fakeHandIns struct {
	mu     sync.Mutex
	calls  []model.HandInCall
	answer model.Inventory
	err    error
	gate   chan struct{}
}

func (f *fakeHandIns) HandIn(ctx context.Context, c model.HandInCall) (model.Inventory, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return model.Inventory{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	return f.answer, f.err
}

func (f *fakeHandIns) called() []model.HandInCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// questTown runs the town with the quest data; the player stands by the town guide, joined through c1.
type questTown struct {
	inst *instance.Instance
	sink *worldtest.Sink
	cps  *worldtest.Checkpoints
	p    model.Player
	data *gamedata.Data
}

func newQuestTown(t *testing.T, level int, log model.Quests, handIns instance.HandIns) *questTown {
	t.Helper()
	data := questData(t)
	cps := &worldtest.Checkpoints{}
	inst, stop := worldtest.Run(t, data, cps, worldtest.Options{CheckpointInterval: time.Hour, HandIns: handIns, ItemCalls: runningPool(t)})
	t.Cleanup(stop)
	p := worldtest.NewPlayer("Ayla", 450, 608)
	p.Level, p.Connection, p.Quests = level, "c1", log
	s := join(t, inst, p)
	cps.Drain()
	return &questTown{inst: inst, sink: s, cps: cps, p: p, data: data}
}

func (q *questTown) accept(quest string) error {
	err := q.inst.AcceptQuest(context.Background(), q.p.ID, "c1", "town_guide", quest)
	tick(1)
	return err
}

func (q *questTown) abandon(quest string) error {
	err := q.inst.AbandonQuest(context.Background(), q.p.ID, "c1", quest)
	tick(1)
	return err
}

func (q *questTown) handIn(quest string) error {
	err := q.inst.HandInQuest(context.Background(), q.p.ID, "c1", "town_guide", quest)
	tick(1)
	return err
}

// saved is the quest log of the one checkpoint since the last call; it fails without exactly one.
func (q *questTown) saved(t *testing.T) model.Quests {
	t.Helper()
	cps := q.cps.Drain()
	if len(cps) != 1 {
		t.Fatalf("%d checkpoints, want one at once", len(cps))
	}
	return cps[0].Quests
}

func changes(s *worldtest.Sink) []model.QuestChanged {
	out := eventsOf[model.QuestChanged](s)
	for k := range out {
		out[k].Tick = 0
	}
	return out
}

func TestAcceptingAQuest(t *testing.T) {
	finished := model.Quest{Status: model.QuestFinished, TimesFinished: 1}
	for _, c := range []struct {
		name  string
		level int
		log   model.Quests
		quest string
		want  error
	}{
		{"one the NPC doesn't give", 1, nil, "nope", model.ErrQuestUnavailable},
		{"one already active", 1, model.Quests{"errand": {Status: model.QuestActive, Progress: []int{0, 0}}}, "errand", model.ErrQuestActive},
		{"a one-time quest finished", 3, model.Quests{"slime_cleanup": finished}, "slime_cleanup", model.ErrQuestFinished},
		{"below its level, its quest first not finished", 1, nil, "sticky_samples", model.ErrQuestUnmet},
		{"with the most active already", 1, twentyActive(), "errand", model.ErrTooManyQuests},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				q := newQuestTown(t, c.level, c.log, nil)
				if err := q.accept(c.quest); !errors.Is(err, c.want) {
					t.Fatalf("err = %v, want %v", err, c.want)
				}
				if got := changes(q.sink); len(got) != 0 || len(q.cps.Drain()) != 0 {
					t.Fatalf("a refusal told %+v or saved", got)
				}
			})
		})
	}
	t.Run("accepted with no progress, told and saved at once", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			q := newQuestTown(t, 2, model.Quests{"slime_cleanup": finished}, nil)
			if err := q.accept("sticky_samples"); err != nil {
				t.Fatal(err)
			}
			want := model.Quest{Status: model.QuestActive, Progress: []int{0}}
			if got := changes(q.sink); !reflect.DeepEqual(got, []model.QuestChanged{{ID: "sticky_samples", Change: model.QuestAccepted, Quest: want}}) {
				t.Fatalf("told %+v", got)
			}
			if got := q.saved(t); !reflect.DeepEqual(got["sticky_samples"], want) || !reflect.DeepEqual(got["slime_cleanup"], finished) {
				t.Fatalf("saved %+v", got)
			}
		})
	})
	t.Run("a repeatable quest again keeps its count", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			q := newQuestTown(t, 1, model.Quests{"bounty": {Status: model.QuestFinished, TimesFinished: 2}}, nil)
			if err := q.accept("bounty"); err != nil {
				t.Fatal(err)
			}
			if got := q.saved(t)["bounty"]; !reflect.DeepEqual(got, model.Quest{Status: model.QuestActive, Progress: []int{0, 0}, TimesFinished: 2}) {
				t.Fatalf("saved %+v", got)
			}
		})
	})
	t.Run("out of reach", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			q := newQuestTown(t, 1, nil, nil)
			worldtest.Walk(q.inst, q.p.ID, 0, sim.Input{Left: true}, 5)
			if err := q.accept("errand"); !errors.Is(err, model.ErrNpcOutOfReach) {
				t.Fatalf("err = %v", err)
			}
		})
	})
}

func twentyActive() model.Quests {
	log := model.Quests{}
	for k := range quests.MaxActive {
		log[fmt.Sprintf("q%d", k)] = model.Quest{Status: model.QuestActive}
	}
	return log
}

func TestAbandoningAQuest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := newQuestTown(t, 1, model.Quests{
			"errand": {Status: model.QuestActive, Progress: []int{1, 0}},
			"bounty": {Status: model.QuestActive, Progress: []int{1, 0}, TimesFinished: 1},
			"q":      {Status: model.QuestFinished, TimesFinished: 1},
		}, nil)
		if err := q.abandon("errand"); err != nil {
			t.Fatal(err)
		}
		if _, kept := q.saved(t)["errand"]; kept {
			t.Fatal("a quest never finished stays in the log")
		}
		if err := q.abandon("bounty"); err != nil {
			t.Fatal(err)
		}
		if got := q.saved(t)["bounty"]; !reflect.DeepEqual(got, model.Quest{Status: model.QuestFinished, TimesFinished: 1}) {
			t.Fatalf("a repeatable quest finished before is %+v", got)
		}
		want := []model.QuestChanged{
			{ID: "errand", Change: model.QuestAbandoned},
			{ID: "bounty", Change: model.QuestAbandoned, Quest: model.Quest{Status: model.QuestFinished, TimesFinished: 1}},
		}
		if got := changes(q.sink); !reflect.DeepEqual(got, want) {
			t.Fatalf("told %+v", got)
		}
		for _, quest := range []string{"q", "nope"} {
			if err := q.abandon(quest); err != nil {
				t.Fatalf("%s: %v", quest, err)
			}
		}
		if len(q.cps.Drain()) != 0 || len(changes(q.sink)) != 2 {
			t.Fatal("abandoning a quest not active changed something")
		}
	})
}

func TestTalkingToTheNPCATalkObjectiveNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := newQuestTown(t, 3, model.Quests{"errand": {Status: model.QuestActive, Progress: []int{0, 0}}}, nil)
		if err := q.inst.Talk(context.Background(), q.p.ID, "c1", "town_guide"); err != nil {
			t.Fatal(err)
		}
		tick(1)
		if got := changes(q.sink); !reflect.DeepEqual(got, []model.QuestChanged{{ID: "errand", Change: model.QuestProgressed, Quest: model.Quest{Status: model.QuestActive, Progress: []int{1, 0}}}}) {
			t.Fatalf("told %+v", got)
		}
		d := eventsOf[model.NpcDialog](q.sink)
		if len(d) != 1 || !reflect.DeepEqual(d[0].Takes, []model.QuestTurnIn{{Quest: "errand", Ready: true}}) {
			t.Fatalf("dialog %+v: the talk counts before the dialog is computed", d)
		}
	})
}

// A kill counts for each player who shares its EXP: the killer and the living party members on the map.
func TestKillsCountForEveryoneWhoSharesTheExp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := questData(t)
		parties := &fakeParties{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Parties: parties})
		defer stop()
		tick(1)
		hunting := func() model.Quests {
			return model.Quests{
				"slime_cleanup": {Status: model.QuestActive, Progress: []int{3}},
				"bounty":        {Status: model.QuestActive, Progress: []int{1, 0}}, // its one slime already
			}
		}
		killer, member, dead := warriorAt(250), warriorAt(150), warriorAt(300)
		killer.Quests, member.Quests, dead.Quests = hunting(), hunting(), hunting()
		dead.HP = 1 // standing in the slime: dies on the first tick
		sinks := map[kernel.CharacterID]*worldtest.Sink{}
		for _, p := range []model.Player{killer, member, dead} {
			sinks[p.ID] = join(t, inst, p)
		}
		parties.mu.Lock()
		parties.parties = [][]kernel.CharacterID{{killer.ID, member.ID, dead.ID}}
		parties.mu.Unlock()
		killSlime(t, inst, killer.ID)
		want := []model.QuestChanged{{ID: "slime_cleanup", Change: model.QuestProgressed, Quest: model.Quest{Status: model.QuestActive, Progress: []int{4}}}}
		for _, p := range []model.Player{killer, member} {
			if got := changes(sinks[p.ID]); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s told %+v, want %+v", p.Name, got, want)
			}
		}
		if got := changes(sinks[dead.ID]); len(got) != 0 {
			t.Fatalf("the dead member told %+v", got)
		}
	})
}

func TestHandingInAQuest(t *testing.T) {
	ready := func(times int) model.Quests {
		return model.Quests{"bounty": {Status: model.QuestActive, Progress: []int{1, 0}, TimesFinished: times}}
	}
	cap := &model.SlotItem{ID: kernel.NewItemID(), Template: "leather_cap", Quantity: 1}
	answer := model.Inventory{Revision: 7, Slots: []model.Slot{{Tab: gamedata.Etc, Slot: 0}, {Tab: gamedata.Equip, Slot: 0, Item: cap}}}

	t.Run("the item service takes and gives, then the quest finishes, awards its EXP and saves", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer}
			q := newQuestTown(t, 1, ready(0), f)
			if err := q.handIn("bounty"); err != nil {
				t.Fatal(err)
			}
			tmpl, _ := q.data.Item("leather_cap")
			roll := progression.Rolled(tmpl.Rolls, func(r gamedata.Range) int { return min(r.Min+int(0.5*float64(r.Max-r.Min+1)), r.Max) })
			want := model.HandInCall{
				Account: q.p.Account, Character: q.p.ID, Key: quests.HandInKey(q.p.ID, "bounty", 1), Quest: "bounty",
				Take: []gamedata.Grant{{Item: "slime_gel", Quantity: 2}},
				Give: []model.Reward{{Template: "leather_cap", Quantity: 1, Stats: &roll}, {Template: "red_potion", Quantity: 3}},
			}
			if got := f.called(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
				t.Fatalf("called %+v, want %+v", got, want)
			}
			done := model.Quest{Status: model.QuestFinished, TimesFinished: 1}
			if got := changes(q.sink); !reflect.DeepEqual(got, []model.QuestChanged{{ID: "bounty", Change: model.QuestHandedIn, Quest: done}}) {
				t.Fatalf("told %+v", got)
			}
			inv := eventsOf[model.InventoryChanged](q.sink)
			if len(inv) != 1 || inv[0].Revision != 7 || !reflect.DeepEqual(inv[0].Slots, answer.Slots) {
				t.Fatalf("inventory changes %+v", inv)
			}
			if exp := eventsOf[model.ExpGained](q.sink); len(exp) != 1 || exp[0].Amount != 4 {
				t.Fatalf("EXP %+v", exp)
			}
			if got := q.saved(t)["bounty"]; !reflect.DeepEqual(got, done) {
				t.Fatalf("saved %+v", got)
			}
		})
	})
	t.Run("a refusal changes nothing, and a hand-in again uses the same key", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{err: errors.New("not enough items")}
			q := newQuestTown(t, 1, ready(0), f)
			if err := q.handIn("bounty"); err == nil || err.Error() != "not enough items" {
				t.Fatalf("err = %v, want the item service's", err)
			}
			if len(changes(q.sink)) != 0 || len(eventsOf[model.ExpGained](q.sink)) != 0 || len(q.cps.Drain()) != 0 {
				t.Fatal("a refused hand-in changed something")
			}
			f.mu.Lock()
			f.err, f.answer = nil, answer
			f.mu.Unlock()
			if err := q.handIn("bounty"); err != nil {
				t.Fatal(err)
			}
			if got := f.called(); len(got) != 2 || got[0].Key != got[1].Key {
				t.Fatalf("keys %+v; the same completion keeps its key", got)
			}
		})
	})
	t.Run("a repeatable quest's next completion has its own key", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer}
			q := newQuestTown(t, 1, ready(1), f)
			if err := q.handIn("bounty"); err != nil {
				t.Fatal(err)
			}
			if got := f.called(); len(got) != 1 || got[0].Key != quests.HandInKey(q.p.ID, "bounty", 2) || got[0].Key == quests.HandInKey(q.p.ID, "bounty", 1) {
				t.Fatalf("called %+v", got)
			}
			if got := q.saved(t)["bounty"]; got.TimesFinished != 2 {
				t.Fatalf("saved %+v", got)
			}
		})
	})
	t.Run("a quest with no items finishes without the item service", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{}
			q := newQuestTown(t, 3, model.Quests{"errand": {Status: model.QuestActive, Progress: []int{1, 0}}}, f)
			if err := q.handIn("errand"); err != nil {
				t.Fatal(err)
			}
			if len(f.called()) != 0 || q.saved(t)["errand"].Status != model.QuestFinished {
				t.Fatal("the errand didn't finish on its own")
			}
			if exp := eventsOf[model.ExpGained](q.sink); len(exp) != 1 || exp[0].Amount != 7 {
				t.Fatalf("EXP %+v", exp)
			}
		})
	})
	t.Run("refused before the call", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			level int
			log   model.Quests
			quest string
			want  error
		}{
			{"a kill left", 1, model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{9}}}, "slime_cleanup", model.ErrQuestNotReady},
			{"a level left", 2, model.Quests{"errand": {Status: model.QuestActive, Progress: []int{1, 0}}}, "errand", model.ErrQuestNotReady},
			{"not active", 1, model.Quests{"bounty": {Status: model.QuestFinished, TimesFinished: 1}}, "bounty", model.ErrQuestNotReady},
			{"one the NPC doesn't take", 1, nil, "nope", model.ErrQuestUnavailable},
		} {
			t.Run(c.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					f := &fakeHandIns{}
					q := newQuestTown(t, c.level, c.log, f)
					if err := q.handIn(c.quest); !errors.Is(err, c.want) || len(f.called()) != 0 {
						t.Fatalf("err = %v after %d calls, want %v", err, len(f.called()), c.want)
					}
				})
			})
		}
	})
	t.Run("while its hand-in is in flight, the quest can't be handed in or abandoned", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer, gate: make(chan struct{})}
			q := newQuestTown(t, 1, ready(0), f)
			first := make(chan error, 1)
			go func() { first <- q.inst.HandInQuest(context.Background(), q.p.ID, "c1", "town_guide", "bounty") }()
			tick(1)
			if err := q.handIn("bounty"); !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("a second hand-in: %v", err)
			}
			if err := q.abandon("bounty"); !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("an abandon: %v", err)
			}
			close(f.gate)
			tick(1)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
		})
	})
}
