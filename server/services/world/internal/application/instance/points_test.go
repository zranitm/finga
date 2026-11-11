package instance_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// atLevel3 is a level 3 warrior in the town holding points, joined through connection c1.
func atLevel3(t *testing.T, points model.Points, hp int) (*instance.Instance, *worldtest.Sink, *worldtest.Checkpoints, model.Player) {
	t.Helper()
	data := worldtest.LoadData(t)
	cps := &worldtest.Checkpoints{}
	inst, stop := worldtest.Run(t, data, cps, worldtest.Options{CheckpointInterval: time.Hour})
	t.Cleanup(stop)
	x, y := worldtest.TownSpawn(t, data)
	p := worldtest.NewPlayer("Ayla", x, y)
	p.Level, p.Connection, p.Points, p.HP = 3, "c1", points, hp
	s := join(t, inst, p)
	cps.Drain()
	return inst, s, cps, p
}

// A level 3 warrior's max HP is 60+18×2, plus 2 a STR and 10 a VIT: 180 with the starting 7 STR and 7 VIT.
func TestSpendingPoints(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, s, cps, p := atLevel3(t, model.Points{Unspent: 10}, 100)
		spend := func(conn string, expected int, add gamedata.Stats) error {
			err := inst.SpendPoints(context.Background(), p.ID, conn, expected, add)
			tick(1)
			return err
		}
		for _, c := range []struct {
			name     string
			expected int
			add      gamedata.Stats
			want     error
		}{
			{"a negative stat", 10, gamedata.Stats{STR: -1, VIT: 2}, model.ErrBadPoints},
			{"no points at all", 10, gamedata.Stats{}, model.ErrBadPoints},
			{"a count already changed", 9, gamedata.Stats{STR: 1}, model.ErrPointsChanged},
			{"more than unspent", 10, gamedata.Stats{STR: 6, AGI: 5}, model.ErrNotEnoughPoints},
		} {
			if err := spend("c1", c.expected, c.add); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.name, err, c.want)
			}
		}
		if got := eventsOf[model.StatsChanged](s); len(got) != 0 || len(cps.Drain()) != 0 {
			t.Fatalf("refusals changed stats %+v or saved", got)
		}

		if err := spend("c1", 10, gamedata.Stats{STR: 4, VIT: 2}); err != nil {
			t.Fatal(err)
		}
		want := model.Points{Spent: gamedata.Stats{STR: 4, VIT: 2}, Unspent: 4}
		changed := eventsOf[model.StatsChanged](s)
		if len(changed) != 1 || changed[0].Sheet.Points != want || changed[0].Sheet.Primary != (gamedata.Stats{STR: 11, DEX: 7, INT: 7, AGI: 7, VIT: 9}) || changed[0].MaxHP != 208 {
			t.Fatalf("stats changed %+v; want 4 more STR and 2 more VIT, 208 max HP", changed)
		}
		if saved := cps.Drain(); len(saved) != 1 || saved[0].Points != want {
			t.Fatalf("checkpoints %+v; want the spend saved at once", saved)
		}
		if v, _ := s.Own(p.ID); v.MaxHP != 208 || v.HP > 101 {
			t.Fatalf("view %+v; want the new max HP to everyone, and HP not refilled", v)
		}
		if err := spend("c2", 4, gamedata.Stats{STR: 1}); !errors.Is(err, transient.ErrUnavailable) {
			t.Fatalf("through another connection: %v", err)
		}
	})
}

// The free reset returns every point the levels earned, once; HP falls to a lower maximum.
func TestResettingPoints(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, s, cps, p := atLevel3(t, model.Points{Spent: gamedata.Stats{VIT: 8}, Unspent: 2}, 300)
		reset := func(expected int) error {
			err := inst.ResetPoints(context.Background(), p.ID, "c1", expected)
			tick(1)
			return err
		}
		if err := reset(1); !errors.Is(err, model.ErrPointsChanged) {
			t.Fatalf("a changed count: %v", err)
		}
		if err := reset(2); err != nil {
			t.Fatal(err)
		}
		want := model.Points{Unspent: 10, FreeResetUsed: true}
		changed := eventsOf[model.StatsChanged](s)
		if len(changed) != 1 || changed[0].Sheet.Points != want || changed[0].MaxHP != 180 || changed[0].HP != 180 {
			t.Fatalf("stats changed %+v; want every point unspent and HP down to 180", changed)
		}
		if saved := cps.Drain(); len(saved) != 1 || saved[0].Points != want || saved[0].HP != 180 {
			t.Fatalf("checkpoints %+v; want the reset saved at once", saved)
		}
		if err := reset(10); !errors.Is(err, model.ErrNoFreeReset) {
			t.Fatalf("a second reset: %v", err)
		}
	})
}

// requiring is the game data with one item needing stats, which items.yaml may not have.
type requiring struct {
	*gamedata.Data
	item  string
	needs gamedata.Stats
}

func (r requiring) Item(id string) (gamedata.Item, bool) {
	it, ok := r.Data.Item(id)
	if id == r.item {
		it.Requires = r.needs
	}
	return it, ok
}

// A worn item below its requirements stays worn and adds nothing; the own stats alone decide, so another item's STR never unlocks it.
func TestStatsOfCountsOnlyTheWornItemsWhoseRequirementsAreMet(t *testing.T) {
	data := worldtest.LoadData(t)
	content := requiring{data, "wooden_sword", gamedata.Stats{STR: 10}}
	warrior, _ := data.Class(kernel.Class("warrior"))
	worn := model.Worn{Items: []model.WornItem{
		{Template: "wooden_sword", Stats: progression.ItemStats{PhysicalAttack: 10}},
		{Template: "leather_cap", Stats: progression.ItemStats{STR: 5, MaxHP: 30}},
		{Template: "no_such_item", Stats: progression.ItemStats{MaxHP: 1000}},
	}}
	short := instance.StatsOf(content, warrior, 3, model.Points{Spent: gamedata.Stats{STR: 2}}, worn, worldtest.TickInterval)
	if w := short.Sheet.DerivedWorn; w.PhysicalAttack != 5*1.5 || short.Own.STR != 9 || short.Sheet.PrimaryWorn.STR != 5 {
		t.Fatalf("own STR 9 and the cap's 5: worn share %+v, own %+v; want the sword left out", w, short.Own)
	}
	met := instance.StatsOf(content, warrior, 3, model.Points{Spent: gamedata.Stats{STR: 3}}, worn, worldtest.TickInterval)
	if w := met.Sheet.DerivedWorn; w.PhysicalAttack != 10+5*1.5 || met.Derived.MaxHP != short.Derived.MaxHP+2 {
		t.Fatalf("own STR 10: worn share %+v; want the sword counted", w)
	}
	if met.Sheet.Points != (model.Points{Spent: gamedata.Stats{STR: 3}}) {
		t.Fatalf("sheet points %+v", met.Sheet.Points)
	}
}
