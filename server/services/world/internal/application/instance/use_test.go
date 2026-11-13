package instance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

var errNotThere = errors.New("not in the inventory")

// fakePotions answers every use with answer, replayed or err, each call waiting for gate when it is set.
type fakePotions struct {
	mu       sync.Mutex
	uses     []model.UseCall
	answer   model.Inventory
	replayed bool
	err      error
	gate     chan struct{}
}

func (f *fakePotions) Use(ctx context.Context, c model.UseCall) (model.Inventory, bool, error) {
	f.mu.Lock()
	f.uses = append(f.uses, c)
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return model.Inventory{}, false, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.answer, f.replayed, f.err
}

func (f *fakePotions) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.uses)
}

// redPotionsLeft is a use's answer: the Use tab's slot 0 holds n red potions, at revision 7.
func redPotionsLeft(n int) model.Inventory {
	return model.Inventory{Revision: 7, Slots: []model.Slot{{Tab: gamedata.Use, Slot: 0, Item: &model.SlotItem{ID: kernel.NewItemID(), Template: "red_potion", Quantity: n}}}}
}

func useAsync(inst *instance.Instance, id kernel.CharacterID, template string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.UseItem(context.Background(), id, "c1", "k-1", kernel.NewItemID(), template) }()
	return done
}

func TestAPotionRestoresHPOnceTheItemServiceTakesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		potions := &fakePotions{answer: redPotionsLeft(4), gate: make(chan struct{})}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.HP, p.Connection = 10, "c1"
		s := join(t, inst, p)

		done := useAsync(inst, p.ID, "red_potion")
		tick(3)
		if v, _ := s.Own(p.ID); v.HP != 10 || len(eventsOf[model.StatsChanged](s)) != 0 {
			t.Fatalf("HP %d before the item service answered", v.HP)
		}
		close(potions.gate)
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if c := potions.uses[0]; c.Character != p.ID || c.Account != p.Account || c.Key != "k-1" || c.Template != "red_potion" {
			t.Fatalf("call %+v", c)
		}
		inv, stats := eventsOf[model.InventoryChanged](s), eventsOf[model.StatsChanged](s)
		if len(inv) != 1 || inv[0].Revision != 7 || inv[0].Slots[0].Item.Quantity != 4 {
			t.Fatalf("inventory changes %+v", inv)
		}
		if len(stats) != 1 || stats[0].HP != 60 || stats[0].Sheet != nil {
			t.Fatalf("stats changes %+v, want HP 10 + 50 and no sheet, which a potion leaves as it was", stats)
		}
		if v, _ := s.Own(p.ID); v.HP != 60 {
			t.Fatalf("HP %d everyone sees, want 60", v.HP)
		}
	})
}

func TestAPotionRestoresNoMoreThanTheMaximum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		potions := &fakePotions{answer: redPotionsLeft(4)}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t)})
		defer stop()
		probe := worldtest.NewPlayer("Probe", 100, 608)
		probe.HP = 1_000_000 // a join holds HP to the maximum
		full, _ := join(t, inst, probe).Own(probe.ID)
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.HP, p.Connection = full.MaxHP-20, "c1"
		s := join(t, inst, p)
		done := useAsync(inst, p.ID, "red_potion")
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if stats := eventsOf[model.StatsChanged](s); len(stats) != 1 || stats[0].HP != full.MaxHP {
			t.Fatalf("stats changes %+v, want HP held to %d", stats, full.MaxHP)
		}
	})
}

func TestAPotionIsRefusedBeforeTheCall(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hp, mp   int
		template string
		want     error
	}{
		{"an HP potion at full HP", 1_000_000, 1, "red_potion", model.ErrHPFull},
		{"an MP potion at full MP", 1, 1_000_000, "blue_potion", model.ErrMPFull},
		{"a material", 1, 1, "slime_gel", model.ErrNotUsable},
		{"equipment", 1, 1, "wooden_sword", model.ErrNotUsable},
		{"a template the data doesn't have", 1, 1, "elixir_of_ages", model.ErrNotUsable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				potions := &fakePotions{answer: redPotionsLeft(4)}
				inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t)})
				defer stop()
				p := worldtest.NewPlayer("Ayla", 100, 608)
				p.HP, p.MP, p.Connection = tc.hp, tc.mp, "c1"
				s := join(t, inst, p)
				done := useAsync(inst, p.ID, tc.template)
				tick(1)
				if err := <-done; !errors.Is(err, tc.want) || potions.calls() != 0 {
					t.Fatalf("err = %v after %d calls, want %v and none", err, potions.calls(), tc.want)
				}
				if n := len(eventsOf[model.StatsChanged](s)) + len(eventsOf[model.InventoryChanged](s)); n != 0 {
					t.Fatalf("%d events after a refusal", n)
				}
			})
		})
	}
	t.Run("for a dead character", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			potions := &fakePotions{answer: redPotionsLeft(4)}
			inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t), Transfer: &transferRecorder{}})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.HP, p.Connection = 1, "c1"
			untilDead(t, join(t, inst, p), p.ID) // the slime's hop kills
			done := useAsync(inst, p.ID, "red_potion")
			tick(1)
			if err := <-done; !errors.Is(err, model.ErrCharacterDead) || potions.calls() != 0 {
				t.Fatalf("err = %v after %d calls, want ErrCharacterDead and none", err, potions.calls())
			}
		})
	})
}

func TestARefusedUseRestoresNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		potions := &fakePotions{err: errNotThere}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.HP, p.Connection = 10, "c1"
		s := join(t, inst, p)
		done := useAsync(inst, p.ID, "red_potion")
		tick(2)
		if err := <-done; !errors.Is(err, errNotThere) {
			t.Fatalf("err = %v, want the item service's refusal", err)
		}
		if v, _ := s.Own(p.ID); v.HP != 10 || len(eventsOf[model.StatsChanged](s))+len(eventsOf[model.InventoryChanged](s)) != 0 {
			t.Fatalf("HP %d and events after a refusal", v.HP)
		}
	})
}

// A retry under a key the item service already took a potion for heals nothing again, so repeating one key gives no free heals.
func TestAReplayedUseRestoresNothingAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		potions := &fakePotions{answer: redPotionsLeft(4), replayed: true}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.HP, p.Connection = 10, "c1"
		s := join(t, inst, p)
		done := useAsync(inst, p.ID, "red_potion")
		tick(2)
		if err := <-done; err != nil {
			t.Fatalf("err = %v, want the replay answered", err)
		}
		if v, _ := s.Own(p.ID); v.HP != 10 || len(eventsOf[model.StatsChanged](s)) != 0 {
			t.Fatalf("HP %d after a replay, want 10", v.HP)
		}
		if inv := eventsOf[model.InventoryChanged](s); len(inv) != 1 || inv[0].Revision != 7 {
			t.Fatalf("inventory changes %+v, want the stored reply told", inv)
		}
	})
}

// A potion the item service took but whose answer was lost heals nothing, and neither does its retry: the potion is lost, by choice.
func TestAPotionTakenButUnansweredIsLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		potions := &fakePotions{err: context.DeadlineExceeded}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.HP, p.Connection = 10, "c1"
		s := join(t, inst, p)
		done := useAsync(inst, p.ID, "red_potion")
		tick(2)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the lost answer", err)
		}
		potions.mu.Lock()
		potions.answer, potions.replayed, potions.err = redPotionsLeft(4), true, nil
		potions.mu.Unlock()
		done = useAsync(inst, p.ID, "red_potion") // the retry, under the same key
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if c := potions.uses; len(c) != 2 || c[0].Key != c[1].Key {
			t.Fatalf("calls %+v, want two under one key", c)
		}
		if v, _ := s.Own(p.ID); v.HP != 10 || len(eventsOf[model.StatsChanged](s)) != 0 {
			t.Fatalf("HP %d after the retry, want 10", v.HP)
		}
	})
}

// A player who dies while the item service takes the potion keeps nothing: the potion is spent, and the dead are not healed.
func TestAPotionSpentAsThePlayerDiesRestoresNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		potions := &fakePotions{answer: redPotionsLeft(4), gate: make(chan struct{})}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t), Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := warriorAt(nextToSlime)
		p.HP, p.Connection = 1, "c1"
		s := join(t, inst, p)
		done := useAsync(inst, p.ID, "red_potion")
		untilDead(t, s, p.ID) // to the slime's hop, with the potion in flight
		if v, _ := s.Own(p.ID); v.HP != 0 {
			t.Fatalf("HP %d, want the player dead before the answer", v.HP)
		}
		close(potions.gate)
		tick(2)
		if err := <-done; !errors.Is(err, model.ErrCharacterDead) {
			t.Fatalf("err = %v, want ErrCharacterDead", err)
		}
		if inv := eventsOf[model.InventoryChanged](s); len(inv) != 1 || inv[0].Revision != 7 {
			t.Fatalf("inventory changes %+v, want the spent potion told", inv)
		}
		if v, _ := s.Own(p.ID); v.HP != 0 || len(eventsOf[model.StatsChanged](s)) != 0 {
			t.Fatalf("HP %d after the answer, want the dead left dead", v.HP)
		}
	})
}
