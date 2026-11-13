package instance_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
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
				inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), still(arena(time.Hour)), nil, worldtest.Options{})
				defer stop()
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				s := join(t, inst, warriorAt(200))
				id := idOf(t, s)
				_ = inst.Input(id, 1, sim.Input{Right: true}, tc.view)
				time.Sleep(25 * worldtest.TickInterval) // held still, then the held input walks on: 120 px, past the slime at 284-316
				synctest.Wait()
				if v, _ := s.Own(id); v.State.X != 272 || v.State.VX != 0 {
					t.Fatalf("the player stood at %+v; want stopped at x 272, the slime's left side", v.State)
				}
			})
		})
	}
}

// still is m as a town, where mobs block and stop at players but never attack.
func still(m *gamedata.Map) *gamedata.Map {
	m.Kind, m.Town = gamedata.Town, m.ID
	return m
}

// walkway is the arena, as a town, with its slime on the floor, walking right from x 600.
func walkway() *gamedata.Map {
	m := still(arena(time.Hour))
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

// C12: a player pressing into a slime walking toward them, their view a few ticks old as a client's is, ends at most the lag's walk into it, never through.
func TestAPlayerAndAMobMeetingUnderLagNeitherPassesThrough(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), walkway(), nil, worldtest.Options{NewRoll: worldtest.Rolls(0.25)}) // the slime at x 508, walking left
		defer stop()
		time.Sleep(10 * worldtest.TickInterval)
		synctest.Wait()
		s := join(t, inst, warriorAt(400))
		id := idOf(t, s)
		for seq := uint32(1); seq <= 60; seq++ {
			_ = inst.Input(id, seq, sim.Input{Right: true}, uint32(s.Last().Tick-3))
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
		}
		var me, slime float64
		met := false
		for _, e := range s.All() {
			var players []model.PlayerView
			var mobs []model.MobView
			switch f := e.(type) {
			case model.Snapshot:
				players, mobs = f.Players, f.Mobs
			case worldtest.Delta:
				players, mobs = f.Players, f.Mobs
			}
			for _, p := range players {
				if p.ID == id {
					me = p.State.X
				}
			}
			for _, mb := range mobs {
				slime = mb.X
			}
			deep := (me + 12) - (slime - 16)
			met = met || deep >= 0
			if deep > 4*1.6 || me >= slime {
				t.Fatalf("the player at x %v is %v px into the slime at x %v; want at most the 4 ticks' walk it made since their view", me, deep, slime)
			}
		}
		if !met {
			t.Fatal("the player never reached the slime")
		}
	})
}

// C12 PR 4: a mob chasing a player past a portal holds at its edge, so the player, against the wall beyond it, can walk back onto it and leave.
func TestAPlayerPinnedPastAPortalCanStillLeaveByIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := arena(time.Hour)
		m.Geometry.Solids = append(m.Geometry.Solids, sim.Rect{X: 1000, Y: 0, W: 32, H: 608})                                                    // a wall
		m.Portals = []gamedata.Portal{{Name: "out", Area: sim.Rect{X: 936, Y: 560, W: 48, H: 48}, TargetMap: "town", TargetPortal: "to_forest"}} // 16 px short of it
		m.Spawns[0].Area.X = 784                                                                                                                 // the slime at 800
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), m, nil, worldtest.Options{Transfer: tr})
		defer stop()
		tick(1)
		p := warriorAt(988) // feet past the portal, back to the wall
		p.HP = 1_000_000
		s := join(t, inst, p)
		tick(100) // 120 px of chase at 1.6 a tick
		for _, mb := range mobsIn(s) {
			if mb.X != 936-16 {
				t.Fatalf("slime at x %v; want it held with its right side at the portal's left edge, 936", mb.X)
			}
		}
		for seq := uint32(1); seq <= 10; seq++ {
			_ = inst.Input(p.ID, seq, sim.Input{Left: true}, 0)
			tick(1)
		}
		if v, _ := s.Own(p.ID); v.State.X != 936+12 {
			t.Fatalf("the player stood at x %v; want stopped at the slime's side, 948, inside the portal", v.State.X)
		}
		_ = inst.EnterPortal(p.ID)
		tick(1)
		if moved(tr) != 1 {
			t.Fatal("the player didn't leave by the portal")
		}
	})
}

// A player joining a map nobody was on is blocked by its mobs from their first input, with no view yet: the empty ticks kept their boxes.
func TestAPlayerJoiningAnEmptyMapIsBlockedAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), still(arena(time.Hour)), nil, worldtest.Options{})
		defer stop()
		tick(2 * mobs.History)             // nobody on the map, so no view can name a tick with a player on it
		s := join(t, inst, warriorAt(270)) // 2 px short of the slime's left side, 284
		id := idOf(t, s)
		_ = inst.Input(id, 1, sim.Input{Right: true}, 0)
		tick(5)
		if v, _ := s.Own(id); v.State.X != 272 {
			t.Fatalf("the player stood at x %v; want stopped at once at the slime's left side, 272", v.State.X)
		}
	})
}
