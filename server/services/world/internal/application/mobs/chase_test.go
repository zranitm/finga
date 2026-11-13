package mobs_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

func groundTo(w float64) func(*gamedata.Map) {
	return func(m *gamedata.Map) { m.Geometry.Platforms[0].W = w }
}

func solid(r model.Rect) func(*gamedata.Map) {
	return func(m *gamedata.Map) { m.Geometry.Solids = append(m.Geometry.Solids, r) }
}

func portal(r model.Rect) func(*gamedata.Map) {
	return func(m *gamedata.Map) {
		m.Portals = append(m.Portals, gamedata.Portal{Name: "out", Area: r, TargetMap: "town", TargetPortal: "to_forest"})
	}
}

// C12 PR 4: a chase goes past the patrol area along the ground, under a ledge and onto a level pillar, until a wall, a block, a portal or its end stops it.
func TestAChaseFollowsItsGroundToAWallABlockAPortalOrItsEnd(t *testing.T) {
	for _, tt := range []struct {
		name  string
		edits []func(*gamedata.Map)
		stop  float64 // where it holds, short of the player at 480; 0 where it gets to them
	}{
		{"the ground's end at 400", []func(*gamedata.Map){groundTo(400)}, 384},
		{"a wall at 400", []func(*gamedata.Map){solid(model.Rect{X: 400, Y: 0, W: 32, H: floor})}, 384},
		{"a block at 400", []func(*gamedata.Map){solid(model.Rect{X: 400, Y: floor - 48, W: 64, H: 48})}, 384},
		{"a portal at 400", []func(*gamedata.Map){portal(model.Rect{X: 400, Y: floor - 48, W: 48, H: 48})}, 384},
		{"a ledge above its head at 400", []func(*gamedata.Map){solid(model.Rect{X: 400, Y: floor - 100, W: 64, H: 64})}, 0}, // 36 px up; the slime is 28 tall
		{"its platform onto a pillar's top at 400", []func(*gamedata.Map){groundTo(400), solid(model.Rect{X: 400, Y: floor, W: 200, H: 200})}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := alone(t, "green_slime", tt.edits...)
			got := play(a, 1, 130, still(standing(480))) // 184 px at 1.6 a tick: at 384 by tick 116
			v := a.Views()[0]
			if tt.stop == 0 {
				if len(got.landed) == 0 || v.X <= maxX {
					t.Fatalf("slime %+v, landings %+v; want it gone on past its area to the player and hopping on them", v, got.landed)
				}
				return
			}
			if v.X != tt.stop || v.Facing != 1 || len(got.started) != 0 {
				t.Fatalf("slime %+v, windups %+v; want it held at x %v, facing the player out of its reach", v, got.started, tt.stop)
			}
			if got := play(a, 131, 131, still(standing(tt.stop-40))); len(got.started) != 1 {
				t.Fatalf("started %+v; want a hop at once at a player come into its reach, a switch of target with no wait", got.started)
			}
		})
	}
}

// C12 PR 4: a leap and a rush carry a mob out of its patrol area along its ground, and stop at its chase bounds: here a block on its right.
func TestALeapAndARushStopAtTheChaseBounds(t *testing.T) {
	a := alone(t, "green_slime", penned(32), solid(model.Rect{X: 260, Y: floor - 40, W: 8, H: 40})) // its chase bounds end at 260 - 16
	p := standing(282)                                                                              // left edge 270: in reach of a hop that lands at 244, 2 px short of its spread's 272
	got := play(a, 1, 21+wait, still(p))
	if v := a.Views()[0]; v.X != 244 || len(got.landed[21+wait]) != 1 {
		t.Fatalf("slime %+v, landed %+v; want its hop aimed 72 px on cut to 244 by the block, landing on the player", v, got.landed)
	}

	a = alone(t, "orange_mushroom", penned(36), solid(model.Rect{X: 300, Y: floor - 40, W: 8, H: 40})) // its chase bounds end at 300 - 18
	got = play(a, 1, 30+wait, func(n uint64) []mobs.Body {
		if n <= 1+wait {
			return []mobs.Body{standing(240)} // in its charge's path for the windup's start, then gone
		}
		return nil
	})
	if v := a.Views()[0]; v.X != 282 || len(got.started) != 1 || len(got.landed) != 0 {
		t.Fatalf("mushroom %+v, windups %+v; want its 128 px charge cut to 282 by the block, meeting nobody", v, got.started)
	}
}

// C12 PR 4: a mob that picks up a target, having had none, starts its first windup after a wait rolled from none to 400 ms.
func TestTheEngageWaitIsRolled(t *testing.T) {
	for _, tt := range []struct {
		roll float64
		at   uint64
	}{{0, 1}, {0.5, 1 + wait}, {0.999, 9}} {
		m := slimes(time.Hour)
		m.Spawns[0].Mob, m.Spawns[0].Max = "wild_boar", 1
		a := mobs.NewArena(mobs.Deps{Map: m, Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: tt.roll}}, mobs.Tuning{TickInterval: tick})
		p := standing(a.Views()[0].X + 40) // in its bite's reach
		if got := play(a, 1, 12, still(p)); len(got.started) != 1 || got.started[0].Tick != tt.at {
			t.Fatalf("at a roll of %v: windups %+v; want the bite at tick %d", tt.roll, got.started, tt.at)
		}
	}
}

// C12 PR 4: mobs that see a player on one tick don't attack as one: each rolls its own wait.
func TestMobsThatSeeAPlayerTogetherWindUpApart(t *testing.T) {
	a := mobs.NewArena(mobs.Deps{Map: slimes(time.Hour), Data: worldtest.LoadData(t), Roll: rand.New(rand.NewPCG(3, 5))}, mobs.Tuning{TickInterval: tick})
	v := a.Views()
	p := standing((v[0].X + v[1].X) / 2) // between them, in both hops' reach
	got := play(a, 1, 12, still(p))
	first := map[uint64]uint64{}
	for _, s := range got.started {
		if _, ok := first[s.Mob]; !ok {
			first[s.Mob] = s.Tick
		}
	}
	if len(first) != 2 || first[v[0].ID] == first[v[1].ID] {
		t.Fatalf("first windups by mob %v; want both slimes winding up, at different ticks", first)
	}
}

// C12 PR 4: only a mob that had no target waits: one that loses its target and takes another attacks the new one at once.
func TestASwitchOfTargetHasNoWait(t *testing.T) {
	a := alone(t, "wild_boar")
	play(a, 1, 9, still(standing(450))) // its target from tick 1, seen out of reach: its wait is over by tick 1+wait
	near := standing(a.Views()[0].X + 40)
	if got := play(a, 10, 10, still(near)); len(got.started) != 1 {
		t.Fatalf("started %+v at tick 10; want a bite at once at the player it switched to", got.started)
	}

	a = alone(t, "wild_boar")
	play(a, 1, 9, still()) // patrolling: no target
	near = standing(a.Views()[0].X + 40)
	if got := play(a, 10, 10+wait, still(near)); len(got.started) != 1 || got.started[0].Tick != 10+wait {
		t.Fatalf("started %+v; want the bite after a wait, at tick %d, as it had no target", got.started, 10+wait)
	}
}

// C12 PR 4: a boss obeys portals too: it follows a player up to a portal's inner edge and holds, though the player stands just past it.
func TestABossHoldsAtAPortalsEdge(t *testing.T) {
	m := slimes(time.Hour)
	m.Spawns = nil
	portal(model.Rect{X: 800, Y: floor - 48, W: 32, H: 48})(m)
	a := mobs.NewArena(mobs.Deps{Map: m, Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
	def, area := king(t)
	a.SpawnBoss(def, area)
	play(a, 1, 200, still(standing(870))) // 152 px from 600 to the portal's edge at 1.2 a tick
	if v := a.Views()[0]; v.X != 800-def.Width/2 || v.Facing != 1 {
		t.Fatalf("king %+v; want it held with its right side at the portal's left edge, facing the player", v)
	}
}

// C12 PR 4: no mob or boss on a shipped map can step onto a portal: wherever a chase takes it on its ground, its box keeps off every portal.
func TestNoShippedMobCanStepOntoAPortal(t *testing.T) {
	data := worldtest.LoadData(t)
	for _, m := range data.Maps() {
		if m.Kind != gamedata.Hunting {
			continue
		}
		a := mobs.NewArena(mobs.Deps{Map: m, Data: data, Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
		if m.Boss != nil {
			def, _ := data.Mob(m.Boss.Mob)
			a.SpawnBoss(def, m.Boss.Area)
		}
		for _, v := range a.Views() {
			def, _ := data.Mob(v.Type)
			lo, hi, _ := a.ChaseBounds(v.ID)
			reach := model.Rect{X: lo - def.Width/2, Y: v.Y - def.Height, W: hi - lo + def.Width, H: def.Height}
			for _, p := range m.Portals {
				if p.Area.Overlaps(reach) {
					t.Errorf("%s: %s at x %v can chase from %v to %v, onto portal %s at %+v", m.ID, v.Type, v.X, lo, hi, p.Name, p.Area)
				}
			}
		}
	}
}
