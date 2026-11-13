package instance_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

var errTooLow = errors.New("level too low")

// fakeEquipment answers every equip and unequip with answer or err, each call waiting for gate when it is set.
type fakeEquipment struct {
	mu       sync.Mutex
	equips   []model.EquipCall
	unequips []model.UnequipCall
	answer   model.Equipped
	err      error
	gate     chan struct{}
}

func (f *fakeEquipment) Equip(ctx context.Context, c model.EquipCall) (model.Equipped, error) {
	f.mu.Lock()
	f.equips = append(f.equips, c)
	f.mu.Unlock()
	return f.wait(ctx)
}

func (f *fakeEquipment) Unequip(ctx context.Context, c model.UnequipCall) (model.Equipped, error) {
	f.mu.Lock()
	f.unequips = append(f.unequips, c)
	f.mu.Unlock()
	return f.wait(ctx)
}

func (f *fakeEquipment) wait(ctx context.Context) (model.Equipped, error) {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return model.Equipped{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.answer, f.err
}

func (f *fakeEquipment) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.equips) + len(f.unequips)
}

// runningPool is an item-call pool whose workers run until the test ends.
func runningPool(t *testing.T) *itemcalls.Pool {
	t.Helper()
	pool := itemcalls.NewPool(itemcalls.Deps{Observer: idleJobs{}, Panics: worldtest.DiscardPanics()}, itemcalls.Options{Workers: 2})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = pool.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return pool
}

// sword is an equip's answer: the sword leaves Equip-tab slot 0 for the weapon slot, giving 12 attack and 30 max HP, at revision 5.
func sword() model.Equipped {
	item := &model.SlotItem{ID: kernel.NewItemID(), Template: "wooden_sword", Quantity: 1, Stats: &progression.ItemStats{PhysicalAttack: 12, MaxHP: 30}}
	return model.Equipped{
		Slots:     []model.Slot{{Tab: gamedata.Equip, Slot: 0}},
		Equipment: []model.EquipSlot{{Slot: "weapon", Item: item}},
		Worn:      model.Worn{Revision: 5, Items: []model.WornItem{{Template: item.Template, Stats: *item.Stats}}},
	}
}

func eventsOf[T any](s *worldtest.Sink) []T {
	var out []T
	for _, e := range s.All() {
		if v, ok := e.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// equipAsync asks inst to equip on its own goroutine and returns where its answer lands.
func equipAsync(inst *instance.Instance, id kernel.CharacterID, conn string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.Equip(context.Background(), id, conn, "k-1", kernel.NewItemID(), "") }()
	return done
}

func tick(n int) {
	time.Sleep(time.Duration(n) * worldtest.TickInterval)
	synctest.Wait()
}

func TestEquipChangesStatsOnlyOnceTheResultArrives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.Connection = "c1"
		s := join(t, inst, p)
		before, _ := s.Own(p.ID)

		done := equipAsync(inst, p.ID, "c1")
		tick(3)
		if got := eventsOf[model.StatsChanged](s); len(got) != 0 {
			t.Fatalf("stats changed before the item service answered: %+v", got)
		}
		if v, _ := s.Own(p.ID); v.MaxHP != before.MaxHP {
			t.Fatalf("max HP %d before the answer, want %d", v.MaxHP, before.MaxHP)
		}
		close(eq.gate)
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if c := eq.equips[0]; c.Character != p.ID || c.Account != p.Account || c.Key != "k-1" || c.Level != 1 || c.Class != kernel.Class("warrior") || c.Stats != (gamedata.Stats{STR: 7, DEX: 7, INT: 7, AGI: 7, VIT: 7}) {
			t.Fatalf("call %+v; want the live level, class and own stats", c)
		}
		inv, stats := eventsOf[model.InventoryChanged](s), eventsOf[model.StatsChanged](s)
		if len(inv) != 1 || inv[0].Revision != 5 || len(inv[0].Equipment) != 1 || inv[0].Equipment[0].Slot != "weapon" || len(inv[0].Slots) != 1 {
			t.Fatalf("inventory changes %+v", inv)
		}
		if len(stats) != 1 || stats[0].Sheet == nil || stats[0].MaxHP != before.MaxHP+30 || stats[0].Sheet.DerivedWorn.PhysicalAttack != 12 || stats[0].Sheet.DerivedWorn.MaxHP != 30 {
			t.Fatalf("stats changes %+v; want 30 more max HP and the sword's 12 attack as the equipment's share", stats)
		}
		if v, _ := s.Own(p.ID); v.MaxHP != before.MaxHP+30 {
			t.Fatalf("max HP %d everyone sees, want %d", v.MaxHP, before.MaxHP+30)
		}
	})
}

func TestARefusedEquipChangesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		eq := &fakeEquipment{err: errTooLow}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.Connection = "c1"
		s := join(t, inst, p)
		done := equipAsync(inst, p.ID, "c1")
		tick(2)
		if err := <-done; !errors.Is(err, errTooLow) {
			t.Fatalf("err = %v, want the item service's refusal", err)
		}
		if got := len(eventsOf[model.StatsChanged](s)) + len(eventsOf[model.InventoryChanged](s)); got != 0 {
			t.Fatalf("%d events after a refusal", got)
		}
	})
}

func TestEquipIsRefusedBeforeTheCall(t *testing.T) {
	t.Run("for a dead character", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			eq := &fakeEquipment{answer: sword()}
			inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: &transferRecorder{}})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.HP, p.Connection = 1, "c1"
			untilDead(t, join(t, inst, p), p.ID) // the slime's hop kills
			done := equipAsync(inst, p.ID, "c1")
			tick(1)
			if err := <-done; !errors.Is(err, model.ErrCharacterDead) || eq.calls() != 0 {
				t.Fatalf("err = %v after %d calls, want ErrCharacterDead and none", err, eq.calls())
			}
		})
	})
	t.Run("from a connection that doesn't hold the character", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			eq := &fakeEquipment{answer: sword()}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t)})
			defer stop()
			p := worldtest.NewPlayer("Ayla", 100, 608)
			p.Connection = "c1"
			join(t, inst, p)
			done := equipAsync(inst, p.ID, "c2")
			tick(1)
			if err := <-done; !errors.Is(err, transient.ErrUnavailable) || eq.calls() != 0 {
				t.Fatalf("err = %v after %d calls, want unavailable and none", err, eq.calls())
			}
		})
	})
}

func TestUnequipLowersHPAndMPToTheNewMaximum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		eq := &fakeEquipment{answer: model.Equipped{Equipment: []model.EquipSlot{{Slot: "hat"}}, Worn: model.Worn{Revision: 4}}}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.Connection, p.Worn = "c1", model.Worn{Revision: 3, Items: []model.WornItem{{Template: "leather_cap", Stats: progression.ItemStats{MaxHP: 50, MaxMP: 10}}}}
		p.HP, p.MP = 194, 51 // full with the hat's 50 HP and 10 MP
		s := join(t, inst, p)
		if v, _ := s.Own(p.ID); v.MaxHP != 194 || v.HP != 194 {
			t.Fatalf("joined %+v; want the hat's 50 HP in a full 194", v)
		}
		if err := inst.Unequip(context.Background(), p.ID, "c1", "k-2", "hat", nil); err != nil {
			t.Fatal(err)
		}
		tick(1)
		stats := eventsOf[model.StatsChanged](s)
		if len(stats) != 1 || stats[0].MaxHP != 144 || stats[0].HP != 144 || stats[0].MaxMP != 41 || stats[0].MP != 41 {
			t.Fatalf("stats %+v; want HP and MP down to the new maximums, 144 and 41", stats)
		}
		if c := eq.unequips[0]; c.EquipSlot != "hat" || c.ToSlot != nil || c.Key != "k-2" {
			t.Fatalf("call %+v", c)
		}
	})
}

// A result older than what the player wears, such as one that arrived after a rejoin read newer, changes nothing.
func TestAStaleResultChangesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		eq := &fakeEquipment{answer: sword()} // revision 5
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t)})
		defer stop()
		p := worldtest.NewPlayer("Ayla", 100, 608)
		p.Connection, p.Worn = "c1", model.Worn{Revision: 7}
		s := join(t, inst, p)
		if err := inst.Equip(context.Background(), p.ID, "c1", "k-1", kernel.NewItemID(), ""); err != nil {
			t.Fatal(err)
		}
		tick(1)
		if got := eventsOf[model.StatsChanged](s); len(got) != 0 {
			t.Fatalf("a stale result changed stats: %+v", got)
		}
	})
}

func TestWornStatsAtJoinAndLevelUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(250)
		p.Exp, p.Worn = 18, model.Worn{Revision: 2, Items: []model.WornItem{{Template: "wooden_sword", Stats: progression.ItemStats{PhysicalAttack: 10}}, {Template: "leather_cap", Stats: progression.ItemStats{MaxHP: 30}}}}
		s := join(t, inst, p)
		if v, _ := s.Own(p.ID); v.MaxHP != 174 {
			t.Fatalf("max HP %d at join, want 144 and the worn 30", v.MaxHP)
		}
		_ = inst.Attack(p.ID, 0) // two hits kill the slime, its 8 EXP a level
		tick(18)
		_ = inst.Attack(p.ID, 0)
		tick(1)
		lus := eventsOf[model.LevelUp](s)
		if len(lus) != 1 || lus[0].MaxHP != 192 || lus[0].HP != 192 || lus[0].Sheet.DerivedWorn.MaxHP != 30 {
			t.Fatalf("level-ups %+v; want max HP 162 and the worn 30, restored, with the equipment's share", lus)
		}
	})
}

// A player whose equip is in flight stays on the map until it settles, so the result applies where the player is.
func TestAnEquipInFlightHoldsThePlayerOnItsMap(t *testing.T) {
	t.Run("past its respawn", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
			tr := &transferRecorder{}
			inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.HP, p.Connection = 1, "c1"
			s := join(t, inst, p)
			done := equipAsync(inst, p.ID, "c1")
			untilDead(t, s, p.ID)               // to the slime's hop, with the equip in flight
			time.Sleep(3500 * time.Millisecond) // past the 3 s respawn, short of the equip's 5 s timeout
			synctest.Wait()
			if moved(tr) != 0 {
				t.Fatal("respawned off the map with an equip in flight")
			}
			close(eq.gate)
			tick(2)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if len(tr.moved) != 1 || !reflect.DeepEqual(tr.moved[0].Worn, sword().Worn) {
				t.Fatalf("moved %+v; want the respawn to carry the sword", tr.moved)
			}
		})
	})
	t.Run("before a portal", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
			tr := &transferRecorder{}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
			defer stop()
			at := data.StartTown().Portals[0].Arrival()
			p := worldtest.NewPlayer("Ayla", at.X, at.Y)
			p.Connection = "c1"
			join(t, inst, p)
			done := equipAsync(inst, p.ID, "c1")
			tick(1)
			_ = inst.EnterPortal(p.ID)
			tick(1)
			if moved(tr) != 0 {
				t.Fatal("took a portal with an equip in flight")
			}
			close(eq.gate)
			tick(1)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			_ = inst.EnterPortal(p.ID)
			tick(1)
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if len(tr.moved) != 1 || !reflect.DeepEqual(tr.moved[0].Worn, sword().Worn) {
				t.Fatalf("moved %+v; want the portal to carry the sword", tr.moved)
			}
		})
	})
}

// The gateway joins a character again through the same connection after a broken stream; an answer to the earlier join's call must not end one of the new join's.
func TestAnAnswerToAnEarlierJoinLeavesTheNewJoinsCallsCounted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		first := make(chan struct{})
		eq := &fakeEquipment{answer: sword(), gate: first}
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
		defer stop()
		at := data.StartTown().Portals[0].Arrival()
		p := worldtest.NewPlayer("Ayla", at.X, at.Y)
		p.Connection = "c1"
		join(t, inst, p)
		earlier := equipAsync(inst, p.ID, "c1")
		tick(1)
		join(t, inst, p)
		close(first)
		tick(1)
		if err := <-earlier; err != nil {
			t.Fatal(err)
		}
		second := make(chan struct{})
		eq.mu.Lock()
		eq.gate = second
		eq.mu.Unlock()
		done := equipAsync(inst, p.ID, "c1")
		tick(1)
		_ = inst.EnterPortal(p.ID)
		tick(1)
		if moved(tr) != 0 {
			t.Fatal("took a portal with an equip in flight")
		}
		close(second)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func moved(tr *transferRecorder) int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return len(tr.moved)
}
