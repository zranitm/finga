package instance_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// hpRises are the ticks a player's HP rose at, each by one, as the Deltas show it.
func hpRises(t *testing.T, s *worldtest.Sink, id kernel.CharacterID) []uint64 {
	t.Helper()
	var ticks []uint64
	last := -1
	for _, d := range s.Deltas() {
		for _, pv := range d.Players {
			if pv.ID != id || pv.HP == last {
				continue
			}
			if last >= 0 {
				if pv.HP != last+1 {
					t.Fatalf("HP went from %d to %d at tick %d; regen adds one at a time", last, pv.HP, d.Tick)
				}
				ticks = append(ticks, d.Tick)
			}
			last = pv.HP
		}
	}
	return ticks
}

// everyApart fails unless ticks are n apart, each on a whole second.
func everyApart(t *testing.T, what string, ticks []uint64, n uint64, want int) {
	t.Helper()
	if len(ticks) != want {
		t.Fatalf("%s rose at ticks %v, want %d rises", what, ticks, want)
	}
	for k, at := range ticks {
		if at%20 != 0 || k > 0 && at-ticks[k-1] != n {
			t.Fatalf("%s rose at ticks %v, want every %d ticks on a whole second", what, ticks, n)
		}
	}
}

// A warrior with VIT 10 and INT 20 regains 0.5 HP and 1 MP a second, in a town too: the fractions carry, so HP rises by one every two seconds and MP every second.
func TestRegenAddsUpExactly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{})
		defer stop()
		x, y := worldtest.TownSpawn(t, data)
		p := worldtest.NewPlayer("Ayla", x, y)
		p.Points = model.Points{Spent: gamedata.Stats{VIT: 3, INT: 13}}
		p.HP, p.MP = 100, 20
		s := join(t, inst, p)
		time.Sleep(20 * time.Second)
		synctest.Wait()

		everyApart(t, "HP", hpRises(t, s, p.ID), 40, 10)
		var mpTicks []uint64
		mp := 20
		for _, c := range worldtest.Of[model.StatsChanged](s) {
			if c.MP != mp+1 || c.MaxMP != 80 || c.HP == 0 {
				t.Fatalf("stats changed %+v after MP %d; want one more MP of 80", c, mp)
			}
			mp = c.MP
			mpTicks = append(mpTicks, c.Tick)
		}
		everyApart(t, "MP", mpTicks, 20, 20)
		if v, _ := s.Own(p.ID); v.HP != 110 {
			t.Fatalf("HP %d after 20 s, want 110", v.HP)
		}
	})
}

// Regen stops at the maximum.
func TestRegenStopsAtTheMaximum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{})
		defer stop()
		x, y := worldtest.TownSpawn(t, data)
		p := worldtest.NewPlayer("Ayla", x, y)
		p.HP, p.MP = 143, 41
		s := join(t, inst, p)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		for _, d := range s.Deltas() {
			for _, pv := range d.Players {
				if pv.ID == p.ID && pv.HP > 144 {
					t.Fatalf("HP %d past its maximum 144 at tick %d", pv.HP, d.Tick)
				}
			}
		}
		if v, _ := s.Own(p.ID); v.HP != 144 {
			t.Fatalf("HP %d, want the maximum 144", v.HP)
		}
		if got := worldtest.Of[model.StatsChanged](s); len(got) != 0 {
			t.Fatalf("stats changed %+v at full MP", got)
		}
	})
}

// The dead regain nothing: HP stays 0 until the respawn.
func TestTheDeadDoNotRegen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP = 1
		s := join(t, inst, p)
		worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		time.Sleep(2500 * time.Millisecond)
		synctest.Wait()
		if len(worldtest.Of[model.Died](s)) != 1 || len(worldtest.Of[model.Respawned](s)) != 0 {
			t.Fatal("want the player dead and not yet respawned")
		}
		for _, d := range s.Deltas() {
			for _, pv := range d.Players {
				if pv.ID == p.ID && pv.Dead && pv.HP != 0 {
					t.Fatalf("HP %d while dead at tick %d", pv.HP, d.Tick)
				}
			}
		}
		if v, _ := s.Own(p.ID); !v.Dead || v.HP != 0 {
			t.Fatalf("view %+v; want dead at 0 HP", v)
		}
	})
}
