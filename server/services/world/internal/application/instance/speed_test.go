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

// A basic attack lasts its class's length: a second press inside the warrior's 900 ms attack swings once it ends, 18 ticks later.
func TestAnAttackLastsItsClassesLength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		_ = inst.Attack(p.ID, 0)
		tick(1)
		_ = inst.Attack(p.ID, 0) // inside the attack: it swings once that ends
		tick(20)
		got := damages(s)
		if len(got) != 2 || got[1].Tick-got[0].Tick != 18 {
			t.Fatalf("hits %+v; want two, 18 ticks apart", got)
		}
	})
}

// The sheet carries the attack's length the world counts, so the client's action clock counts the same ticks.
func TestTheSheetCarriesTheAttacksLength(t *testing.T) {
	data := worldtest.LoadData(t)
	warrior, _ := data.Class(kernel.Class("warrior"))
	if got := instance.StatsOf(data, warrior, 1, model.Points{}, model.Skills{}, model.Worn{}, progression.ItemStats{}, worldtest.TickInterval).Sheet.AttackLengthTicks; got != 18 {
		t.Fatalf("an attack of %d ticks, want the warrior's 18", got)
	}
}

// Haste speeds its caster on foot: a point adds 8.5% move speed to the skills' share of the sheet, and a new thief walks 1.095 times a town's pace with it, 1.01 without.
func TestHasteSpeedsItsCasterOnFoot(t *testing.T) {
	walked := func(t *testing.T, haste bool) (d, moveSpeed float64) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := worldtest.NewPlayer("Ayla", 700, 608)
			p.Class, p.Level, p.MP, p.Connection = kernel.Class("thief"), 10, 1000, "c1"
			p.Skills = model.Skills{Points: map[string]int{"haste": 1}}
			s := join(t, inst, p)
			var seq uint32
			if haste {
				if err := cast(inst, p.ID, "haste"); err != nil {
					t.Fatal(err)
				}
				h, _ := data.Skill("haste")
				seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, int(model.TicksIn(h.Stages[0].Length, worldtest.TickInterval))+1) // standing until its action ends
				changed := sheets(s)
				if len(changed) != 1 {
					t.Fatalf("stats changed %+v, want Haste's sheet", changed)
				}
				moveSpeed = changed[0].Sheet.DerivedSkills.MoveSpeed
			}
			before, _ := s.Own(p.ID)
			worldtest.Walk(inst, p.ID, seq, sim.Input{Right: true}, 6)
			after, _ := s.Own(p.ID)
			d = after.State.X - before.State.X
		})
		return d, moveSpeed
	}
	base, _ := walked(t, false)
	fast, bonus := walked(t, true)
	if math.Abs(bonus-0.085) > 1e-9 || base <= 0 || math.Abs(fast/base-1.095/1.01) > 1e-6 {
		t.Fatalf("Haste's share %v, and walked %v px with it against %v without; want 0.085, and 1.095 times a town's pace against 1.01", bonus, fast, base)
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
