package instance_test

import (
	"math"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// A sword with 50% attack speed takes the warrior's 600 ms cooldown to 400 ms: 8 ticks, not 12.
func TestAttackSpeedShortensTheCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Worn.Items[0].Stats.AttackSpeed = 500
		s := join(t, inst, p)
		_ = inst.Attack(p.ID)
		tick(1)
		_ = inst.Attack(p.ID) // inside the cooldown: it swings once that ends
		tick(10)
		got := damages(s)
		if len(got) != 2 || got[1].Tick-got[0].Tick != 8 {
			t.Fatalf("hits %+v; want two, 8 ticks apart", got)
		}
	})
}

// The sheet carries the cooldown the world counts, so the client's attack gate counts the same ticks.
func TestTheSheetCarriesTheAttackCooldown(t *testing.T) {
	data := worldtest.LoadData(t)
	warrior, _ := data.Class(kernel.Class("warrior"))
	plain := instance.StatsOf(data, warrior, 1, model.Points{}, model.Skills{}, model.Worn{}, worldtest.TickInterval)
	fast := instance.StatsOf(data, warrior, 1, model.Points{}, model.Skills{}, model.Worn{Items: []model.WornItem{{Template: "wooden_sword", Stats: progression.ItemStats{AttackSpeed: 500}}}}, worldtest.TickInterval)
	if plain.Sheet.AttackCooldownTicks != 12 || fast.Sheet.AttackCooldownTicks != 8 {
		t.Fatalf("cooldowns %d and %d ticks, want 12 and 8", plain.Sheet.AttackCooldownTicks, fast.Sheet.AttackCooldownTicks)
	}
}

// Boots with 30% move speed walk 1.3 times as far as a town's pace in a hunting map, a new warrior's 7 AGI 1.01 times, and both at the town's pace in a town.
func TestMoveSpeedCountsInHuntingMapsOnly(t *testing.T) {
	walked := func(t *testing.T, m func(*gamedata.Data) *gamedata.Map, moveSpeed int) float64 {
		t.Helper()
		var d float64
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			inst, stop := worldtest.RunMap(t, data, m(data), nil, worldtest.Options{})
			defer stop()
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			x, y := 700.0, 608.0
			if mm := m(data); mm.Kind == gamedata.Town {
				x, y = mm.PlayerSpawn.X, mm.PlayerSpawn.Y
			}
			p := worldtest.NewPlayer("Ayla", x, y)
			p.Worn = model.Worn{Revision: 1, Items: []model.WornItem{{Template: "leather_cap", Stats: progression.ItemStats{MoveSpeed: moveSpeed}}}}
			s := join(t, inst, p)
			before, _ := s.Own(p.ID)
			worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 6)
			after, _ := s.Own(p.ID)
			d = after.State.X - before.State.X
		})
		return d
	}
	hunting := func(*gamedata.Data) *gamedata.Map { return arena(time.Hour) }
	town := func(d *gamedata.Data) *gamedata.Map { return d.StartTown() }
	pace, townFast := walked(t, town, 0), walked(t, town, 300)
	if pace <= 0 || townFast != pace {
		t.Fatalf("in town: walked %v px with the boots, %v without; want the same", townFast, pace)
	}
	if base, fast := walked(t, hunting, 0), walked(t, hunting, 300); math.Abs(base-1.01*pace) > 1e-6 || math.Abs(fast-1.3*pace) > 1e-6 {
		t.Fatalf("in a hunting map: walked %v px without the boots and %v with them, against the town's %v; want 1.01 and 1.3 times as far", base, fast, pace)
	}
}
