package world_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// fakeHandedIns answers found, or err, and records each ask.
type fakeHandedIns struct {
	found []model.HandedIn
	err   error

	mu    sync.Mutex
	asked [][]model.Completion
}

func (f *fakeHandedIns) HandedIn(_ context.Context, _ kernel.CharacterID, asked []model.Completion) ([]model.HandedIn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, asked)
	return f.found, f.err
}

func (f *fakeHandedIns) asks() [][]model.Completion {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

func TestAJoinFinishesWhatTheItemServiceHandedIn(t *testing.T) {
	account := kernel.NewAccountID()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Warrior, Level: 1, HP: 100, MP: 28, MapID: "town", X: 416, Y: 608,
		Quests: model.Quests{
			"slime_cleanup":  {Status: model.QuestActive, Progress: []int{10}},
			"sticky_samples": {Status: model.QuestFinished, TimesFinished: 1},
		},
	}
	potions := model.Inventory{Revision: 9, Slots: []model.Slot{{Tab: gamedata.Use, Slot: 0, Item: &model.SlotItem{ID: kernel.NewItemID(), Template: "red_potion", Quantity: 10}}}}
	run := func(t *testing.T, f *fakeHandedIns, saver *fakeSaver) func(conn string) (*worldtest.Sink, error) {
		t.Helper()
		chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla}}
		w := worldtest.NewWorld(worldtest.LoadData(t), saver, worldtest.Options{Characters: chars, HandedIns: f})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
		return func(conn string) (*worldtest.Sink, error) {
			s := &worldtest.Sink{}
			_, err := w.Join(context.Background(), account, ayla.ID, conn, s)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			return s, err
		}
	}
	handedIn := func(s *worldtest.Sink) (quests []model.QuestChanged, items []model.InventoryChanged, exp []model.ExpGained) {
		for _, e := range s.All() {
			switch e := e.(type) {
			case model.QuestChanged:
				e.Tick = 0
				quests = append(quests, e)
			case model.InventoryChanged:
				items = append(items, e)
			case model.ExpGained:
				exp = append(exp, e)
			}
		}
		return quests, items, exp
	}

	t.Run("asks about each active quest's next completion, and finishes the one handed in", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandedIns{found: []model.HandedIn{{Quest: "slime_cleanup", Completion: 1, Items: potions}}}
			join := run(t, f, &fakeSaver{})
			s, err := join("c1")
			if err != nil {
				t.Fatal(err)
			}
			if got := f.asks(); !reflect.DeepEqual(got, [][]model.Completion{{{Quest: "slime_cleanup", Completion: 1}}}) {
				t.Fatalf("asked %+v", got)
			}
			quests, items, exp := handedIn(s)
			want := []model.QuestChanged{{ID: "slime_cleanup", Change: model.QuestHandedIn, Quest: model.Quest{Status: model.QuestFinished, TimesFinished: 1}}}
			if !reflect.DeepEqual(quests, want) || len(items) != 1 || items[0].Revision != 9 || !reflect.DeepEqual(items[0].Slots, potions.Slots) || len(exp) != 1 || exp[0].Amount != 30 {
				t.Fatalf("quests %+v, items %+v, EXP %+v", quests, items, exp)
			}
		})
	})
	t.Run("a join again before the save lands finishes it no more", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := &fakeHandedIns{found: []model.HandedIn{{Quest: "slime_cleanup", Completion: 1, Items: potions}}}
			join := run(t, f, &fakeSaver{hangs: 1000})
			if _, err := join("c1"); err != nil {
				t.Fatal(err)
			}
			s, err := join("c2")
			if err != nil {
				t.Fatal(err)
			}
			if got := f.asks(); len(got) != 1 {
				t.Fatalf("asked %+v; the second join's log has the quest finished", got)
			}
			if quests, items, exp := handedIn(s); len(quests)+len(items)+len(exp) != 0 {
				t.Fatalf("quests %+v, items %+v, EXP %+v", quests, items, exp)
			}
		})
	})
	t.Run("a join fails while the item service can't say", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			join := run(t, &fakeHandedIns{err: fmt.Errorf("%w: down", transient.ErrUnavailable)}, &fakeSaver{})
			if _, err := join("c1"); !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("err = %v", err)
			}
		})
	})
}
