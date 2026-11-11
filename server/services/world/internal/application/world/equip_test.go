package world_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/world/internal/application/world"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

type fakeWardrobe struct {
	worn model.Worn
	err  error
}

func (f fakeWardrobe) Worn(context.Context, kernel.CharacterID) (model.Worn, error) {
	return f.worn, f.err
}

// capEquipment wears a cap in the hat slot, giving 30 max HP, at revision 4, and refuses every unequip.
type capEquipment struct{}

func (capEquipment) Equip(context.Context, model.EquipCall) (model.Equipped, error) {
	return model.Equipped{Equipment: []model.EquipSlot{{Slot: "hat", Item: &model.SlotItem{ID: kernel.NewItemID(), Template: "leather_cap", Quantity: 1}}}, Worn: model.Worn{Revision: 4, Items: []model.WornItem{{Template: "leather_cap", Stats: progression.ItemStats{MaxHP: 30}}}}}, nil
}

func (capEquipment) Unequip(context.Context, model.UnequipCall) (model.Equipped, error) {
	return model.Equipped{}, errors.New("not here")
}

func wearingWorld(t *testing.T, ayla model.Character, o worldtest.Options) (*world.World, func()) {
	t.Helper()
	o.Characters = &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla}}
	o.Starter = &fakeStarter{}
	w := worldtest.NewWorld(worldtest.LoadData(t), &fakeSaver{}, o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	return w, func() { cancel(); <-done }
}

func aylaInTown() model.Character {
	return model.Character{
		ID: kernel.NewCharacterID(), Account: kernel.NewAccountID(), Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
		HP: 999, MP: 28, MapID: "town", X: 416, Y: 608,
	}
}

func TestAJoinWearsWhatTheItemServiceSays(t *testing.T) {
	ayla := aylaInTown()
	t.Run("derived with it, the view carrying the sheet", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			w, stop := wearingWorld(t, ayla, worldtest.Options{Wardrobe: fakeWardrobe{worn: model.Worn{Revision: 3, Items: []model.WornItem{{Template: "leather_cap", Stats: progression.ItemStats{MaxHP: 30}}}}}})
			defer stop()
			view, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c1", &worldtest.Sink{})
			if err != nil {
				t.Fatal(err)
			}
			if view.MaxHP != 174 || view.HP != 174 || view.Sheet.DerivedWorn.MaxHP != 30 || view.Sheet.Derived.MaxHP != 174 {
				t.Fatalf("view %+v; want 144 HP and the worn 30, in the sheet too", view)
			}
		})
	})
	t.Run("and fails while it can't say, for the gateway to retry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			down := fmt.Errorf("%w: item service down", transient.ErrUnavailable)
			w, stop := wearingWorld(t, ayla, worldtest.Options{Wardrobe: fakeWardrobe{err: down}})
			defer stop()
			s := &worldtest.Sink{}
			if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c1", s); !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("err = %v, want unavailable", err)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if len(s.All()) != 0 {
				t.Fatalf("joined anyway: %+v", s.All())
			}
		})
	})
}

func TestEquipGoesToTheCharactersMap(t *testing.T) {
	ayla := aylaInTown()
	synctest.Test(t, func(t *testing.T) {
		w, stop := wearingWorld(t, ayla, worldtest.Options{Equipment: capEquipment{}})
		defer stop()
		s := &worldtest.Sink{}
		if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c1", s); err != nil {
			t.Fatal(err)
		}
		if err := w.Equip(context.Background(), ayla.ID, "c2", "k-1", kernel.NewItemID(), ""); !errors.Is(err, transient.ErrUnavailable) {
			t.Fatalf("from another connection: %v, want unavailable", err)
		}
		if err := w.Equip(context.Background(), ayla.ID, "c1", "k-1", kernel.NewItemID(), ""); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		var stats []model.StatsChanged
		for _, e := range s.All() {
			if sc, ok := e.(model.StatsChanged); ok {
				stats = append(stats, sc)
			}
		}
		if len(stats) != 1 || stats[0].MaxHP != 174 {
			t.Fatalf("stats %+v; want the cap's 30 HP on top of 144", stats)
		}
		if err := w.Unequip(context.Background(), ayla.ID, "c1", "k-2", "hat", nil); err == nil {
			t.Fatal("an unequip the item service refused answered ok")
		}
	})
}
