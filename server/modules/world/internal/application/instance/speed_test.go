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
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
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
			p.Class, p.Level, p.Energy, p.Connection = kernel.Class("thief"), 10, 10, "c1"
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

// Haste switches its caster's pace on the schedule the client predicts: pressed during input S, its hit lands that tick, so input S+1 is the first at its speed, and as expireBuffs runs after the 400th tick's movement, input S+401 is the first back at the sheet's. Inputs here go one a tick, so ticks and inputs agree.
func TestHasteSwitchesThePaceOnTheClientsSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(time.Hour)
		m.Spawns = nil // nothing in the way
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", 400, 608)
		p.Class, p.Level, p.Energy, p.Connection = kernel.Class("thief"), 10, 10, "c1"
		p.Skills = model.Skills{Points: map[string]int{"haste": 1}}
		s := join(t, inst, p)
		const pressed = 5
		h, _ := data.Skill("haste")
		end := pressed + model.TicksIn(h.Stages[0].Duration, worldtest.TickInterval) // 400 on
		cast := model.TicksIn(h.Stages[0].Length, worldtest.TickInterval)
		var haste <-chan error
		for seq := uint32(1); uint64(seq) <= end+5; seq++ {
			in := sim.Input{}
			switch {
			case seq <= pressed+40:
				in.Right = true
			case uint64(seq) >= end-5:
				in.Left = true // back the way it came, across the end
			}
			_ = inst.Input(p.ID, seq, in, 0)
			if seq == pressed {
				haste = useDuring(inst, p.ID, "haste", pressed)
			}
			tick(1)
		}
		if err := <-haste; err != nil {
			t.Fatal(err)
		}
		x := map[uint32]float64{}
		var hitAck uint32
		used := eventsOf[model.SkillUsed](s)
		for _, d := range s.Deltas() {
			for _, v := range d.Players {
				if v.ID == p.ID {
					x[d.AckSeq] = v.State.X
				}
			}
			if len(used) == 1 && d.Tick == used[0].Tick {
				hitAck = d.AckSeq
			}
		}
		if hitAck != pressed {
			t.Fatalf("Haste landed with input %d applied, want %d: pressed during it, it lands that tick", hitAck, pressed)
		}
		dx := func(seq uint64) float64 { return math.Abs(x[uint32(seq)] - x[uint32(seq-1)]) } //nolint:gosec // small seqs
		base, slowed, fast := dx(pressed), dx(pressed+1), dx(pressed+cast+1)
		for _, c := range []struct {
			name string
			seq  uint64
			want float64
		}{
			{"the input it was pressed during, at the sheet's pace", pressed, base},
			{"the first after, at Haste's, halved while casting", pressed + 1, fast / 2},
			{"the last cast input", pressed + cast, slowed},
			{"the last hasted input", end, fast},
			{"the first after the end, at the sheet's again", end + 1, base},
		} {
			if got := dx(c.seq); math.Abs(got-c.want) > 1e-9 {
				t.Errorf("%s: input %d walked %v px, want %v", c.name, c.seq, got, c.want)
			}
		}
		if math.Abs(fast/base-1.095/1.01) > 1e-6 {
			t.Fatalf("walked %v px a tick hasted against %v; want 1.095 times a town's pace against 1.01", fast, base)
		}
	})
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
