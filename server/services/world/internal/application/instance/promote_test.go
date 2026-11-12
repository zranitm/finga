package instance_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// fakeGifts records each gift and answers with answer or err, once gate (if set) opens.
type fakeGifts struct {
	mu     sync.Mutex
	calls  []model.GiftCall
	answer model.Inventory
	err    error
	gate   chan struct{}
}

func (f *fakeGifts) Give(ctx context.Context, c model.GiftCall) (model.Inventory, error) {
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

func (f *fakeGifts) called() []model.GiftCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// trainee is a level 10 Beginner beside the Warrior Trainer meeting every condition: STR 30, 10 points in Strike and the Warrior's Trial finished.
func trainee() model.Player {
	p := worldtest.NewPlayer("Ayla", 1120, 608)
	p.Class, p.Level, p.Connection, p.HP, p.MP = "beginner", 10, "c1", 50, 5
	p.Points = model.Points{Spent: gamedata.Stats{STR: 23}}
	p.Skills = model.Skills{Points: map[string]int{"strike": 10}}
	p.Quests = model.Quests{"warriors_trial": {Status: model.QuestFinished, TimesFinished: 1}}
	return p
}

// promotionTown runs the town with gifts, a party service, an hour between checkpoints and a running item-call pool, and joins p.
func promotionTown(t *testing.T, p model.Player, gifts *fakeGifts) (*instance.Instance, *worldtest.Sink, *worldtest.Checkpoints, *fakeParties) {
	t.Helper()
	cps := &worldtest.Checkpoints{}
	parties := &fakeParties{}
	inst, stop := worldtest.Run(t, worldtest.LoadData(t), cps, worldtest.Options{Gifts: gifts, Parties: parties, CheckpointInterval: time.Hour, ItemCalls: runningPool(t)})
	t.Cleanup(stop)
	s := join(t, inst, p)
	cps.Drain()
	return inst, s, cps, parties
}

func promoteAsync(inst *instance.Instance, id kernel.CharacterID, npc string, class kernel.Class) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.Promote(context.Background(), id, "c1", npc, class) }()
	return done
}

func TestATrainersDialogOffersItsPromotion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := trainee()
		p.Points = model.Points{Spent: gamedata.Stats{STR: 20}}
		inst, s, _, _ := promotionTown(t, p, &fakeGifts{})
		if err := inst.Talk(context.Background(), p.ID, "c1", "warrior_trainer"); err != nil {
			t.Fatal(err)
		}
		tick(1)
		dialogs := eventsOf[model.NpcDialog](s)
		if len(dialogs) != 1 || dialogs[0].Promotion == nil {
			t.Fatalf("dialogs %+v, want one with the promotion", dialogs)
		}
		o := dialogs[0].Promotion
		if o.Class != "warrior" || len(o.Conditions) != 4 || o.Conditions[1] != (model.Condition{Kind: model.ConditionStat, ID: "str", Need: 30, Have: 27}) || o.Met() {
			t.Fatalf("offer %+v, want the Warrior's four conditions, STR 27 of 30", o)
		}
	})
}

func TestAPromotion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		p := trainee()
		sword := model.Slot{Tab: gamedata.Equip, Slot: 3, Item: &model.SlotItem{ID: kernel.NewItemID(), Template: "wooden_sword", Quantity: 1}}
		gifts := &fakeGifts{answer: model.Inventory{Revision: 9, Slots: []model.Slot{sword}}}
		inst, s, cps, parties := promotionTown(t, p, gifts)
		done := promoteAsync(inst, p.ID, "warrior_trainer", "warrior")
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}

		tmpl, _ := data.Item("wooden_sword")
		roll := progression.Rolled(tmpl.Rolls, func(r gamedata.Range) int { return (r.Min + r.Max) / 2 })
		want := model.GiftCall{Account: p.Account, Character: p.ID, Key: uuidv7.Derived(uuid.UUID(p.ID), "promotion:warrior"), Template: "wooden_sword", Stats: &roll}
		if got := gifts.called(); len(got) != 1 || got[0].Key != want.Key || got[0].Template != want.Template || *got[0].Stats != roll || got[0].Character != p.ID {
			t.Fatalf("gifts %+v, want %+v", got, want)
		}
		if inv := eventsOf[model.InventoryChanged](s); len(inv) != 1 || inv[0].Revision != 9 || inv[0].Slots[0] != sword {
			t.Fatalf("inventory changed %+v, want the sword at revision 9", inv)
		}
		promoted := eventsOf[model.Promoted](s)
		warrior, _ := data.Class("warrior")
		sheet := instance.StatsOf(data, warrior, 10, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval)
		if len(promoted) != 1 || promoted[0].Class != "warrior" || promoted[0].HP != sheet.Derived.MaxHP || promoted[0].MP != sheet.Derived.MaxMP ||
			promoted[0].Sheet.AttackLengthTicks != sheet.Sheet.AttackLengthTicks {
			t.Fatalf("promoted %+v, want a Warrior at full HP and MP with the Warrior's attack", promoted)
		}
		if v, _ := s.Own(p.ID); v.Class != "warrior" {
			t.Fatalf("the map sees %+v, want a Warrior", v)
		}
		parties.mu.Lock()
		ups := slices.Clone(parties.updates)
		parties.mu.Unlock()
		if last := ups[len(ups)-1]; last.Class != "warrior" || last.HP != last.MaxHP {
			t.Fatalf("the party last heard %+v, want a Warrior at full HP", last)
		}
		if saved := cps.Drain(); len(saved) != 1 || saved[0].Class != "warrior" || saved[0].Skills.Points["strike"] != 10 {
			t.Fatalf("checkpoints %+v, want one at once as a Warrior, Strike's points kept", saved)
		}
	})
}

func TestAPromotionIsRefused(t *testing.T) {
	for _, c := range []struct {
		name  string
		edit  func(*model.Player)
		npc   string
		class kernel.Class
		want  error
	}{
		{"away from the trainer", func(p *model.Player) { p.X = 450 }, "warrior_trainer", "warrior", model.ErrNpcOutOfReach},
		{"a class the trainer doesn't train", func(*model.Player) {}, "warrior_trainer", "thief", model.ErrNotYourNextClass},
		{"already promoted", func(p *model.Player) { p.Class = "warrior" }, "warrior_trainer", "warrior", model.ErrNotYourNextClass},
		{"below level 10", func(p *model.Player) { p.Level = 9 }, "warrior_trainer", "warrior", model.ErrPromotionUnmet},
		{"STR 29", func(p *model.Player) { p.Points.Spent.STR = 22 }, "warrior_trainer", "warrior", model.ErrPromotionUnmet},
		{"9 points in Strike", func(p *model.Player) { p.Skills.Points["strike"] = 9 }, "warrior_trainer", "warrior", model.ErrPromotionUnmet},
		{"the trial taken again but never finished", func(p *model.Player) {
			p.Quests = model.Quests{"warriors_trial": {Status: model.QuestActive, Progress: []int{0}}}
		}, "warrior_trainer", "warrior", model.ErrPromotionUnmet},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := trainee()
				c.edit(&p)
				gifts := &fakeGifts{}
				inst, s, cps, _ := promotionTown(t, p, gifts)
				done := promoteAsync(inst, p.ID, c.npc, c.class)
				tick(1)
				if err := <-done; !errors.Is(err, c.want) {
					t.Fatalf("promote: %v, want %v", err, c.want)
				}
				if len(gifts.called()) != 0 || len(eventsOf[model.Promoted](s)) != 0 || len(cps.Drain()) != 0 {
					t.Fatal("a refused promotion gave, changed or saved something")
				}
			})
		})
	}
}

// The gift refused, as with no room in the bag, changes nothing; one in flight turns a second promotion away; and a condition lost while it's in flight, as to a stat reset, refuses the promotion, the weapon given, so promoting later gives no second one.
func TestAPromotionAroundItsGift(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			p := trainee()
			full := errors.New("inventory full")
			inst, s, cps, _ := promotionTown(t, p, &fakeGifts{err: full})
			done := promoteAsync(inst, p.ID, "warrior_trainer", "warrior")
			tick(2)
			if err := <-done; !errors.Is(err, full) {
				t.Fatalf("promote: %v, want the item service's refusal", err)
			}
			if v, _ := s.Own(p.ID); v.Class != "beginner" || len(cps.Drain()) != 0 {
				t.Fatalf("after a refused gift: %+v", v)
			}
		})
	})
	t.Run("in flight, then a stat reset", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			p := trainee()
			gifts := &fakeGifts{gate: make(chan struct{})}
			inst, s, _, _ := promotionTown(t, p, gifts)
			done := promoteAsync(inst, p.ID, "warrior_trainer", "warrior")
			tick(1)
			again := promoteAsync(inst, p.ID, "warrior_trainer", "warrior")
			tick(1)
			if err := <-again; !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("a second promotion in flight: %v, want unavailable", err)
			}
			reset := make(chan error, 1)
			go func() { reset <- inst.ResetPoints(context.Background(), p.ID, "c1", 0) }()
			tick(1)
			if err := <-reset; err != nil {
				t.Fatal(err)
			}
			close(gifts.gate)
			tick(2)
			if err := <-done; !errors.Is(err, model.ErrPromotionUnmet) {
				t.Fatalf("promote: %v, want refused for the STR the reset took", err)
			}
			if v, _ := s.Own(p.ID); v.Class != "beginner" || len(gifts.called()) != 1 {
				t.Fatalf("after a reset in flight: %+v, %d gifts", v, len(gifts.called()))
			}
		})
	})
}
