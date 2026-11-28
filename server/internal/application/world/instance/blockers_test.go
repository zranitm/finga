package instance_test

import (
	"math"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
)

// touching is a player's and a slime's radii together: their centres' distance when their bodies meet.
const touching = 0.375 + 0.5

// slimeOf is the one mob s last saw.
func slimeOf(t *testing.T, s *worldtest.Sink) model.MobView {
	t.Helper()
	for _, mb := range mobsIn(s) {
		return mb
	}
	t.Fatal("no mob seen")
	return model.MobView{}
}

// C12: a made-up old or future view blocks like the oldest or newest frame kept, never like none.
func TestAPlayerStopsAtAMobsSide(t *testing.T) {
	for _, tc := range []struct {
		name string
		view uint32
	}{{"no view yet", 0}, {"a view long gone", 1}, {"a view not yet sent", 1 << 30}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), still(arena(t, time.Hour)), nil, worldtest.Options{})
				defer stop()
				tick(1)
				s := join(t, inst, warriorAt(slimeZ-3))
				id := idOf(t, s)
				_ = inst.Input(id, 1, sim.Input{Up: true}, tc.view)
				tick(25) // held still, then the held input walks on: 6 m, past the slime
				v, _ := s.Own(id)
				if mb := slimeOf(t, s); math.Abs(v.State.Z-(wire(mb.Z)-touching)) > 1e-9 || v.State.VZ != 0 {
					t.Fatalf("the player stood at %+v; want stopped at z %v, the slime's south side", v.State, wire(mb.Z)-touching)
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

// wire is v as the wire gives it, at the centimetre: a mob's body is where the frame's number puts it.
func wire(v float64) float64 { return quant.Metres(quant.Position(v)) }

// meetZ is the z a player walking along x = arenaX stops at, coming at a slime at (x, z) from the side dir says: +1 north of it, -1 south.
func meetZ(x, z, dir float64) float64 {
	x, z = wire(x), wire(z)
	return z + dir*math.Sqrt(touching*touching-(x-arenaX)*(x-arenaX))
}

// C12: a player pressing into a slime walking away stops at its side in the frame their input names, not where it is now.
func TestAPlayerStopsWhereTheirViewDrewAMovingMob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		watcher := join(t, inst, warriorAt(slimeZ-9)) // in the slime's sight: it walks south after them
		tick(5)
		start := slimeOf(t, watcher)
		s := join(t, inst, warriorAt(start.Z+touching+0.5)) // half a metre short of the slime's north side
		id := idOf(t, s)
		views := map[uint32]uint64{}
		for seq := uint32(1); seq <= 6; seq++ {
			views[seq] = s.Last().Tick - 3
			_ = inst.Input(id, seq, sim.Input{Down: true}, uint32(views[seq]))
			tick(1)
		}
		slimeAt := map[uint64]model.MobView{}
		for _, d := range watcher.Deltas() {
			for _, mb := range d.Mobs {
				slimeAt[d.Tick] = mb
			}
		}
		last := s.Last()
		viewed, now := slimeAt[views[last.AckSeq]], slimeAt[last.Tick]
		if viewed.Z == now.Z {
			t.Fatalf("the slime stood at z %v in the view and now; want it walking", now.Z)
		}
		if v, _ := s.Own(id); math.Abs(v.State.Z-meetZ(viewed.X, viewed.Z, 1)) > 1e-9 {
			t.Fatalf("the player stood at z %v; want %v, at the slime's north side as the view drew it at z %v (now %v)", v.State.Z, meetZ(viewed.X, viewed.Z, 1), viewed.Z, now.Z)
		}
	})
}

// C12: a player pressing into a mob coming at them, their view a few ticks old as a client's is, ends at most the lag's walk into it, never through.
func TestAPlayerAndAMobMeetingUnderLagNeitherPassesThrough(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(t, time.Hour)
		m.Spawns[0].Mob = "wild_boar" // a bite moves nothing, so only walking brings them together
		boar, _ := data.Mob("wild_boar")
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(slimeZ - 8)
		p.HP = 1_000_000
		s := join(t, inst, p)
		for seq := uint32(1); seq <= 60; seq++ {
			_ = inst.Input(p.ID, seq, sim.Input{Up: true}, uint32(s.Last().Tick-3))
			tick(1)
		}
		reach, lag := 0.375+boar.Radius, 4*boar.Speed*worldtest.TickInterval.Seconds()*1.2 // its fastest pace
		var meX, meZ float64
		var mob model.MobView
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
			for _, pv := range players {
				if pv.ID == p.ID {
					meX, meZ = pv.State.X, pv.State.Z
				}
			}
			for _, mb := range mobs {
				mob = mb
			}
			deep := reach - math.Hypot(meX-mob.X, meZ-mob.Z)
			met = met || deep >= -1e-9
			if deep > lag || meZ >= mob.Z {
				t.Fatalf("the player at (%v, %v) is %v m into the boar at (%v, %v); want at most the 4 ticks' walk it made since their view", meX, meZ, deep, mob.X, mob.Z)
			}
		}
		if !met {
			t.Fatal("the player never reached the boar")
		}
	})
}

// C12 PR 4: a mob chasing a player never steps into a portal, so the player, cornered beyond it, can always leave by it.
func TestAPlayerPinnedPastAPortalCanStillLeaveByIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := arena(t, time.Hour)
		m.Geometry.Boxes = append(m.Geometry.Boxes, sim.Box{X: 20, Z: 32, HalfW: 20, HalfD: 1, UX: 1, Top: 4}) // a wall from z 31
		m.Portals = []gamedata.Portal{{Name: "out", At: gamedata.Point{X: 20, Z: 28.5}, Radius: 1, TargetMap: "crossroads", TargetPortal: "from_heights"}}
		m.Spawns[0].At = gamedata.Point{X: 20, Z: 24}
		worldtest.Gridded(t, m)
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), m, nil, worldtest.Options{Transfer: tr})
		defer stop()
		tick(1)
		p := warriorAt(30.6) // feet past the portal, back to the wall
		p.HP = 1_000_000
		s := join(t, inst, p)
		for range 100 {
			tick(1)
			for _, mb := range mobsIn(s) {
				if d := math.Hypot(mb.X-20, mb.Z-28.5); d < 1+0.5-1e-9 {
					t.Fatalf("the slime at (%v, %v) stepped into the portal", mb.X, mb.Z)
				}
			}
		}
		for seq := uint32(1); seq <= 30; seq++ { // toward the portal's centre, round the slime
			v, _ := s.Own(p.ID)
			if math.Hypot(v.State.X-20, v.State.Z-28.5) < 0.5 {
				break
			}
			_ = inst.Input(p.ID, seq, sim.Input{Left: v.State.X > 20.2, Right: v.State.X < 19.8, Down: v.State.Z > 28.5}, 0)
			tick(1)
		}
		_ = inst.EnterPortal(p.ID)
		tick(1)
		if moved(tr) != 1 {
			v, _ := s.Own(p.ID)
			t.Fatalf("the player at %+v didn't leave by the portal", v.State)
		}
	})
}

// A player joining a map nobody was on is blocked by its mobs from their first input, with no view yet: the empty ticks kept them.
func TestAPlayerJoiningAnEmptyMapIsBlockedAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), still(arena(t, time.Hour)), nil, worldtest.Options{})
		defer stop()
		tick(2 * mobs.History)                                   // nobody on the map, so no view can name a tick with a player on it
		s := join(t, inst, warriorAt(slimeStands-touching-0.06)) // 6 cm short of the slime's south side
		id := idOf(t, s)
		_ = inst.Input(id, 1, sim.Input{Up: true}, 0)
		tick(5)
		if v, _ := s.Own(id); math.Abs(v.State.Z-(wire(slimeStands)-touching)) > 1e-9 {
			t.Fatalf("the player stood at z %v; want stopped at once at the slime's south side, %v", v.State.Z, wire(slimeStands)-touching)
		}
	})
}

// A crowd of every mob attack round a player who walks among it, their inputs naming the frame three ticks back as a client's do: whatever bites, hops, charges, spits or knocks them about, no mob ends a tick inside the player.
func TestNoMobEndsATickInsideAPlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := worldtest.Gridded(t, &gamedata.Map{
			ID: "crowd", Kind: gamedata.Hunting, Town: "willowmere", Geometry: worldtest.Flat(40),
			Spawns: []gamedata.Spawn{
				{Mob: "green_slime", Max: 2, Respawn: time.Hour, At: gamedata.Point{X: 20, Z: 23}, Radius: 1.5},
				{Mob: "wild_boar", Max: 2, Respawn: time.Hour, At: gamedata.Point{X: 17, Z: 20}, Radius: 1.5},
				{Mob: "orange_mushroom", Max: 2, Respawn: time.Hour, At: gamedata.Point{X: 23, Z: 18}, Radius: 1.5},
				{Mob: "cave_bat", Max: 2, Respawn: time.Hour, At: gamedata.Point{X: 20, Z: 26}, Radius: 1.5},
			},
			PlayerSpawn: gamedata.Point{X: 20, Z: 3},
		})
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(20)
		p.Level, p.HP = 60, 5000 // lives through the crowd
		s := join(t, inst, p)
		defs := map[string]gamedata.Mob{}
		for _, id := range []string{"green_slime", "wild_boar", "orange_mushroom", "cave_bat"} {
			defs[id], _ = data.Mob(id)
		}
		ways := []sim.Input{{Up: true}, {Right: true}, {Down: true}, {Left: true}, {}, {Up: true, Left: true}, {Down: true, Right: true}}
		var seq uint32
		for n := range 400 {
			seq++
			_ = inst.Input(p.ID, seq, ways[(n/12)%len(ways)], uint32(s.Last().Tick-min(s.Last().Tick, 3)))
			tick(1)
			me, _ := s.Own(p.ID)
			if me.Dead {
				t.Fatalf("the player died at tick %d; want them alive through the test", s.Last().Tick)
			}
			for _, mb := range mobsIn(s) {
				d := math.Hypot(mb.X-me.State.X, mb.Z-me.State.Z)
				def := defs[mb.Type]
				over := me.State.Y < mb.Y+def.Height && mb.Y < me.State.Y+1.75
				if want := def.Radius + 0.375; over && d < want-1e-9 {
					t.Fatalf("tick %d: a %s %.3f m from the player; want %v or more", s.Last().Tick, mb.Type, d, want)
				}
			}
		}
		attacked := map[string]bool{}
		types := mobsIn(s)
		for _, a := range worldtest.Of[model.MobAttacked](s) {
			attacked[types[a.Mob].Type] = true
		}
		knocked := 0
		for _, d := range damages(s) {
			if d.Source.IsMob() && !d.Miss {
				knocked++
			}
		}
		if len(attacked) != 4 || knocked < 4 {
			t.Fatalf("attacks by %v and %d knockbacks; want every kind, and knockbacks among them", attacked, knocked)
		}
	})
}
