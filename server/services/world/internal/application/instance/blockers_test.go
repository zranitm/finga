package instance_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/services/world/worldtest"
)

// C12: a made-up old or future view blocks like the oldest or newest frame kept, never like none.
func TestAPlayerStopsAtAMobsSide(t *testing.T) {
	for _, tc := range []struct {
		name string
		view uint32
	}{{"no view yet", 0}, {"a view long gone", 1}, {"a view not yet sent", 1 << 30}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				s := join(t, inst, warriorAt(200))
				id := idOf(t, s)
				_ = inst.Input(id, 1, sim.Input{Right: true}, tc.view)
				time.Sleep(25 * worldtest.TickInterval) // held still, then the held input walks on: 120 px, past the slime on its platform at 284-316
				synctest.Wait()
				if v, _ := s.Own(id); v.State.X != 272 || v.State.VX != 0 {
					t.Fatalf("the player stood at %+v; want stopped at x 272, the slime's left side", v.State)
				}
			})
		})
	}
}

// walkway is the arena with its slime on the floor, walking right from x 600.
func walkway() *gamedata.Map {
	m := arena(time.Hour)
	m.Spawns[0].Area = sim.Rect{X: 400, Y: 580, W: 400, H: 28}
	return m
}

// C12: a player pressing into a slime walking away stops at its side in the frame their input names, not where it is now.
func TestAPlayerStopsWhereTheirViewDrewAMovingMob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), walkway(), nil, worldtest.Options{})
		defer stop()
		time.Sleep(10 * worldtest.TickInterval)
		synctest.Wait()
		watcher := join(t, inst, warriorAt(100))
		var start float64
		for _, mb := range mobsIn(watcher) {
			start = mb.X
		}
		s := join(t, inst, warriorAt(start-16-12-20)) // 20 px short of the slime's left side
		id := idOf(t, s)
		views := map[uint32]uint64{}
		for seq := uint32(1); seq <= 6; seq++ {
			views[seq] = s.Last().Tick - 3
			_ = inst.Input(id, seq, sim.Input{Right: true}, uint32(views[seq]))
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
		}
		slimeAt := map[uint64]float64{}
		for _, d := range watcher.Deltas() {
			for _, mb := range d.Mobs {
				slimeAt[d.Tick] = mb.X
			}
		}
		last := s.Last()
		viewed, now := slimeAt[views[last.AckSeq]], slimeAt[last.Tick]
		if viewed == now {
			t.Fatalf("the slime stood at %v in the view and now; want it walking", now)
		}
		if v, _ := s.Own(id); v.State.X != viewed-16-12 {
			t.Fatalf("the player stood at x %v; want %v, at the slime's left side as the view drew it at x %v (now %v)", v.State.X, viewed-16-12, viewed, now)
		}
	})
}
