package mobs_test

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/modules/world/internal/application/mobs"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
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

// C12 PR 4: a chase goes past the patrol area along the ground, under a ledge and onto a level pillar, until a wall, a block or its end stops it.
func TestAChaseFollowsItsGroundToAWallABlockOrItsEnd(t *testing.T) {
	for _, tt := range []struct {
		name  string
		edits []func(*gamedata.Map)
		stop  float64 // where it holds, short of the player at 480; 0 where it gets to them
	}{
		{"the ground's end at 400", []func(*gamedata.Map){groundTo(400)}, 384},
		{"a wall at 400", []func(*gamedata.Map){solid(model.Rect{X: 400, Y: 0, W: 32, H: floor})}, 384},
		{"a block at 400", []func(*gamedata.Map){solid(model.Rect{X: 400, Y: floor - 48, W: 64, H: 48})}, 384},
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
			if got := play(a, 131, 131+wait, still(standing(tt.stop-40))); len(got.started) != 1 || got.started[0].Tick != 131+wait {
				t.Fatalf("started %+v; want a hop at a player come into its reach, after its wait", got.started)
			}
		})
	}
}

// C12 PR 4: a leap and a rush carry a mob out of its patrol area along its ground, and stop at a block on its right: its body, and its hit with it.
func TestALeapAndARushStopAtABlock(t *testing.T) {
	a := alone(t, "green_slime", penned(32), solid(model.Rect{X: 260, Y: floor - 40, W: 8, H: 40})) // its bounds end at 260 - 16
	p := standing(282)                                                                              // left edge 270: in reach of a hop that lands at 244, 2 px short of its spread's 272
	got := play(a, 1, 21+wait, still(p))
	if v := a.Views()[0]; v.X != 244 || len(got.landed[21+wait]) != 1 {
		t.Fatalf("slime %+v, landed %+v; want its hop aimed 72 px on cut to 244 by the block, landing on the player", v, got.landed)
	}

	a = alone(t, "orange_mushroom", penned(36), solid(model.Rect{X: 300, Y: floor - 40, W: 8, H: 40})) // its bounds end at 300 - 18
	behind := standing(330)                                                                            // left edge 318: where its charge would sweep, 128 px on, but for the block
	got = play(a, 1, 30+wait, func(n uint64) []mobs.Body {
		if n <= 1+wait {
			return []mobs.Body{standing(240), behind} // the nearer in its charge's path for the windup's start, then gone
		}
		return []mobs.Body{behind}
	})
	if v := a.Views()[0]; v.X != 282 || len(got.started) != 1 || len(got.landed) != 0 {
		t.Fatalf("mushroom %+v, windups %+v, landed %+v; want its 128 px charge cut to 282 by the block, missing the player behind it", v, got.started, got.landed)
	}
}

// C12 PR 4 follow-up: a mob holds at a portal's edge, but its hop or charge reaches across: a player on the portal comes into its reach, and after its wait it lands.
func TestAMobHeldAtAPortalsEdgeHitsAcrossIt(t *testing.T) {
	for _, tt := range []struct {
		id   string
		edge float64 // where its body holds, short of the portal at 400
		to   float64 // where its hit goes, as its windup tells clients
		land uint64  // when its attack lands, its windup at 131+wait
	}{
		{"green_slime", 400 - 16, 440, 131 + wait + 12 + 8},           // aimed at the player's centre; a 600 ms windup, then 400 ms in the air
		{"orange_mushroom", 400 - 18, 382 + 128, 131 + wait + 16 + 2}, // its 128 px on; an 800 ms windup, then 16 px a tick: its front meets the player's side at 428 on the second
	} {
		t.Run(tt.id, func(t *testing.T) {
			a := alone(t, tt.id, portal(model.Rect{X: 400, Y: floor - 48, W: 48, H: 48}))
			got := play(a, 1, 130, still(standing(560))) // past the portal, out of reach of the hop's 100 px and the charge's 146
			if v := a.Views()[0]; v.X != tt.edge || len(got.started) != 0 {
				t.Fatalf("%s %+v, windups %+v; want it held at the portal's edge, %v, out of reach", tt.id, v, got.started, tt.edge)
			}
			on := standing(440) // 40 px onto the portal
			got = play(a, 131, tt.land+1, still(on))
			if len(got.started) != 1 || got.started[0].Tick != 131+wait || got.started[0].To != tt.to {
				t.Fatalf("started %+v; want one windup at the player come into its reach across the portal, after its wait, its hit going to %v", got.started, tt.to)
			}
			if ticks := got.landedTicks(); !slices.Equal(ticks, []uint64{tt.land}) || !slices.Equal(got.landed[tt.land][0].Hits, []mobs.Hit{{Player: on.ID, Dir: 1}}) {
				t.Fatalf("landed %+v; want it on the player on the portal at tick %d", got.landed, tt.land)
			}
			if v := a.Views()[0]; v.X != tt.edge {
				t.Fatalf("%s at x %v; want its body still at the portal's edge, %v", tt.id, v.X, tt.edge)
			}
		})
	}
}

// C12 PR 4: a mob with a player come into its reach, after nobody was, starts its windup after a wait rolled from none to 400 ms.
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

// C12 PR 4: a mob waits only when someone comes into its reach after nobody was; switching to another player in reach adds no wait.
func TestASwitchOfTargetInReachHasNoWait(t *testing.T) {
	a := alone(t, "wild_boar")
	first, second := standing(spawnX+40), standing(spawnX+40) // both in its bite's reach; first, found first, is its target
	got := play(a, 1, 12, func(n uint64) []mobs.Body {
		if n <= 2 {
			return []mobs.Body{first, second}
		}
		return []mobs.Body{second} // first gone at tick 3: it switches to second, still in reach
	})
	if len(got.started) != 1 || got.started[0].Tick != 1+wait {
		t.Fatalf("windups %+v; want the bite at tick %d, the wait from tick 1, none rolled again at the switch", got.started, 1+wait)
	}
}

// C12 PR 4: mobs holding a target out of reach that comes into reach of them all on one tick each roll their own wait.
func TestAPileComingIntoReachTogetherWindsUpApart(t *testing.T) {
	a := mobs.NewArena(mobs.Deps{Map: slimes(time.Hour), Data: worldtest.LoadData(t), Roll: rand.New(rand.NewPCG(3, 5))}, mobs.Tuning{TickInterval: tick})
	v := a.Views()
	mid := (v[0].X + v[1].X) / 2
	above := up(standing(mid), 88) // seen, out of reach: both walk under them and stand
	got := play(a, 1, 80, still(above))
	if len(got.started) != 0 {
		t.Fatalf("windups %+v; want none at a player out of reach above", got.started)
	}
	got = play(a, 81, 100, still(at(standing(mid), mid))) // down on the floor among them at tick 81
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

// C12 PR 4: a mob whose target leaves its reach, as a knockback carries them, and comes back waits again before its next windup.
func TestAMobWaitsAgainWhenItsTargetComesBackIntoReach(t *testing.T) {
	a := alone(t, "green_slime", pinned)
	p := standing(spawnX + 20) // in its hop's reach; pinned, the slime stays at spawnX
	got := play(a, 1, 70, func(n uint64) []mobs.Body {
		if n > 20 && n <= 60 {
			return []mobs.Body{at(p, 450)} // out of its reach, still in sight
		}
		return []mobs.Body{p}
	})
	var ticks []uint64
	for _, s := range got.started {
		ticks = append(ticks, s.Tick)
	}
	if want := []uint64{1 + wait, 61 + wait}; !slices.Equal(ticks, want) {
		t.Fatalf("windups at ticks %v; want %v: its hop ready from tick %d, but a wait rolled again when the player came back at 61", ticks, want, 1+wait+50)
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
