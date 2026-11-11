package instance_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	down   int // the first down calls find the item service unavailable
}

func (f *fakeHandIns) HandIn(ctx context.Context, c model.HandInCall) (model.Inventory, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return model.Inventory{}, fmt.Errorf("%w: %w", transient.ErrUnavailable, ctx.Err()) // as the client wraps a timeout
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if len(f.calls) <= f.down {
		return model.Inventory{}, fmt.Errorf("%w: the item service is down", transient.ErrUnavailable)
	}
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
	return questTownWith(t, level, log, worldtest.Options{HandIns: handIns})
}

// questTownWith is newQuestTown over o, which gets an hour between checkpoints and a running item-call pool.
func questTownWith(t *testing.T, level int, log model.Quests, o worldtest.Options) *questTown {
	t.Helper()
	data := questData(t)
	cps := &worldtest.Checkpoints{}
	o.CheckpointInterval, o.ItemCalls = time.Hour, runningPool(t)
	inst, stop := worldtest.Run(t, data, cps, o)
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
		{"with the most active already", 1, mostActive(worldtest.LoadData(t).MaxActiveQuests()), "errand", model.ErrTooManyQuests},
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

func mostActive(n int) model.Quests {
	log := model.Quests{}
	for k := range n {
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
				Account: q.p.Account, Character: q.p.ID, Quest: "bounty", Completion: 1,
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
	t.Run("a refusal changes nothing, and a hand-in again is the same completion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{err: errors.New("not enough items")}
			q := newQuestTown(t, 1, ready(0), f)
			if err := q.handIn("bounty"); err == nil || err.Error() != "not enough items" || len(f.called()) != 1 {
				t.Fatalf("err = %v after %d calls, want the item service's after one", err, len(f.called()))
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
			if got := f.called(); len(got) != 2 || got[0].Completion != 1 || got[1].Completion != 1 {
				t.Fatalf("called %+v; both are the first completion", got)
			}
		})
	})
	t.Run("a try the item service was down for is tried again, as the same completion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer, down: 2}
			q := newQuestTown(t, 1, ready(0), f)
			if err := q.handIn("bounty"); err != nil {
				t.Fatal(err)
			}
			if got := f.called(); len(got) != 3 || got[0].Completion != 1 || !reflect.DeepEqual(got[0], got[2]) {
				t.Fatalf("called %+v", got)
			}
			if got := changes(q.sink); len(got) != 1 || got[0].Change != model.QuestHandedIn || q.saved(t)["bounty"].TimesFinished != 1 {
				t.Fatalf("told %+v", got)
			}
		})
	})
	t.Run("an item service down past the tries leaves the quest as it was, and answers within the gateway's 5 s", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			f := &fakeHandIns{answer: answer, down: 1000}
			q := questTownWith(t, 1, ready(0), worldtest.Options{HandIns: f, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
			start := time.Now()
			err := q.inst.HandInQuest(context.Background(), q.p.ID, "c1", "town_guide", "bounty")
			if took := time.Since(start); !errors.Is(err, transient.ErrUnavailable) || len(f.called()) < 2 || took > 4*time.Second {
				t.Fatalf("err = %v after %d calls and %s", err, len(f.called()), took)
			}
			tick(1)
			if len(changes(q.sink)) != 0 || len(q.cps.Drain()) != 0 {
				t.Fatal("something changed")
			}
			if n := strings.Count(logs.String(), `"msg":"hand-in try failed; trying again"`); n == 0 || n > len(f.called()) || !strings.Contains(logs.String(), `"level":"ERROR","msg":"hand-in given up`) {
				t.Fatalf("%d calls, logged:\n%s", len(f.called()), logs.String())
			}
		})
	})
	t.Run("a hung item service is given up within the gateway's 5 s too", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer, gate: make(chan struct{})}
			q := newQuestTown(t, 1, ready(0), f)
			start := time.Now()
			err := q.inst.HandInQuest(context.Background(), q.p.ID, "c1", "town_guide", "bounty")
			if took := time.Since(start); !errors.Is(err, transient.ErrUnavailable) || took < 3*time.Second || took > 4*time.Second {
				t.Fatalf("err = %v after %s; want a try again past the first 1.5 s, given up by 4 s", err, took)
			}
			close(f.gate)
			tick(1)
		})
	})
	t.Run("an answer after the character joined again through another connection finishes the quest for it", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer, gate: make(chan struct{})}
			q := newQuestTown(t, 1, ready(0), f)
			first := make(chan error, 1)
			go func() { first <- q.inst.HandInQuest(context.Background(), q.p.ID, "c1", "town_guide", "bounty") }()
			tick(1)
			again := q.p
			again.Connection, again.Quests = "c2", ready(0)
			s := join(t, q.inst, again)
			close(f.gate)
			tick(1)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			if got := changes(s); len(got) != 1 || got[0].Change != model.QuestHandedIn {
				t.Fatalf("told %+v", got)
			}
			if inv := eventsOf[model.InventoryChanged](s); len(inv) != 1 || inv[0].Revision != 7 {
				t.Fatalf("inventory changes %+v", inv)
			}
			if cps := q.cps.Drain(); len(cps) == 0 || cps[len(cps)-1].Quests["bounty"].TimesFinished != 1 {
				t.Fatalf("saved %+v", cps)
			}
		})
	})
	t.Run("an answer after the character left the map changes nothing here", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer, gate: make(chan struct{})}
			q := newQuestTown(t, 1, ready(0), f)
			first := make(chan error, 1)
			go func() { first <- q.inst.HandInQuest(context.Background(), q.p.ID, "c1", "town_guide", "bounty") }()
			tick(1)
			if err := q.inst.Leave(q.p.ID, "c1", nil); err != nil {
				t.Fatal(err)
			}
			tick(1)
			close(f.gate)
			tick(1)
			if err := <-first; !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("err = %v", err)
			}
			if len(changes(q.sink)) != 0 {
				t.Fatal("the quest changed")
			}
		})
	})
	t.Run("a join finishes each completion the item service handed in, unless the log moved past it", func(t *testing.T) {
		for _, c := range []struct {
			name     string
			log      model.Quests
			handedIn []model.HandedIn
			finished int // the completion it finishes; 0 none
		}{
			{"the next completion", ready(0), []model.HandedIn{{Quest: "bounty", Completion: 1, Items: answer}}, 1},
			{"a repeatable quest's next", ready(2), []model.HandedIn{{Quest: "bounty", Completion: 3, Items: answer}}, 3},
			{"one already finished", model.Quests{"bounty": {Status: model.QuestFinished, TimesFinished: 1}}, []model.HandedIn{{Quest: "bounty", Completion: 1, Items: answer}}, 0},
			{"one past the next", ready(0), []model.HandedIn{{Quest: "bounty", Completion: 2, Items: answer}}, 0},
			{"a quest not in the log", nil, []model.HandedIn{{Quest: "bounty", Completion: 1, Items: answer}}, 0},
		} {
			t.Run(c.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var logs bytes.Buffer
					cps := &worldtest.Checkpoints{}
					inst, stop := worldtest.Run(t, questData(t), cps, worldtest.Options{CheckpointInterval: time.Hour, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
					defer stop()
					p := worldtest.NewPlayer("Ayla", 450, 608)
					p.Connection, p.Quests, p.HandedIn = "c1", c.log, c.handedIn
					s := join(t, inst, p)
					got := changes(s)
					if logged := strings.Contains(logs.String(), `"msg":"a hand-in the world missed finished at the join"`); logged != (c.finished > 0) {
						t.Fatalf("logged %s", logs.String())
					}
					if c.finished == 0 {
						if len(got) != 0 || len(eventsOf[model.InventoryChanged](s)) != 0 {
							t.Fatalf("told %+v", got)
						}
						return
					}
					done := model.Quest{Status: model.QuestFinished, TimesFinished: c.finished}
					if !reflect.DeepEqual(got, []model.QuestChanged{{ID: "bounty", Change: model.QuestHandedIn, Quest: done}}) {
						t.Fatalf("told %+v", got)
					}
					if inv := eventsOf[model.InventoryChanged](s); len(inv) != 1 || inv[0].Revision != 7 {
						t.Fatalf("inventory changes %+v", inv)
					}
					if saved := cps.Drain(); len(saved) != 1 || !reflect.DeepEqual(saved[0].Quests["bounty"], done) {
						t.Fatalf("saved %+v", saved)
					}
				})
			})
		}
	})
	t.Run("a repeatable quest hands in its next completion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandIns{answer: answer}
			q := newQuestTown(t, 1, ready(1), f)
			if err := q.handIn("bounty"); err != nil {
				t.Fatal(err)
			}
			if got := f.called(); len(got) != 1 || got[0].Completion != 2 {
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

// A change in the tick a player leaves the map still reaches them: the hand-off sends it with the tick's other private events.
func TestAQuestChangeInThePortalsTickReachesThePlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := questData(t)
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{CheckpointInterval: time.Hour, ItemCalls: runningPool(t), Transfer: tr})
		defer stop()
		at := data.StartTown().Portals[0].Arrival()
		p := worldtest.NewPlayer("Ayla", at.X, at.Y)
		p.Connection, p.Quests = "c1", model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{3}}}
		s := join(t, inst, p)
		done := make(chan error, 1)
		go func() { done <- inst.AbandonQuest(context.Background(), p.ID, "c1", "slime_cleanup") }()
		synctest.Wait()
		_ = inst.EnterPortal(p.ID)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if moved(tr) != 1 {
			t.Fatal("the portal wasn't taken in the abandon's tick")
		}
		if got, want := changes(s), []model.QuestChanged{{ID: "slime_cleanup", Change: model.QuestAbandoned}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("quest changes %+v, want %+v", got, want)
		}
	})
}
