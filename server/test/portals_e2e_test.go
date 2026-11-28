package e2e_test

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestTravel(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, id := s.CreateCharacter(t, "sam", "Sam", "warrior")
	p, snap := s.Join(t, token, id)
	l := e2etest.NewLooter(p, snap)
	l.Await(300*time.Millisecond, nil)

	t.Run("the-town-is-safe", func(t *testing.T) {
		start, _ := load.Dir(s.Data) // the mobs in view stand in hunting areas, none in the town
		m, _ := start.Map("willowmere")
		for _, mob := range snap.Mobs {
			if area := m.AreaGrid.Holding(float64(mob.X), float64(mob.Z)); area >= 0 && m.Areas[area].Kind == gamedata.Town {
				t.Fatalf("a mob at (%v, %v) stands in the town %s", mob.X, mob.Z, m.Areas[area].ID)
			}
		}
		if err := l.Attack(0); err != nil {
			t.Fatal(err)
		}
		l.Await(500*time.Millisecond, func(m wsconn.Message) bool {
			if m.Type == "Damage" {
				t.Fatalf("damage in the town: %+v", m.Data)
			}
			return false
		})
	})

	t.Run("off-a-portal-EnterPortal-does-nothing", func(t *testing.T) {
		if err := l.EnterPortal(); err != nil {
			t.Fatal(err)
		}
		if l.Await(500*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "MapChange" }) {
			t.Fatal("a map change from the town spawn")
		}
	})

	t.Run("through-the-cave-gate-and-back", func(t *testing.T) {
		// Gate centres, where hops start and arrivals land; Tia stands by the cliffs' gate, as a walk there would end.
		token, id := s.CreateCharacter(t, "tia", "Tia", "warrior")
		s.Place(t, id, "willowmere", gamedata.Point{X: e2etest.Spots(t).CaveGate.X - 2, Z: e2etest.Spots(t).CaveGate.Z}, 144, 0)
		p, snap := s.Join(t, token, id)
		tia := e2etest.NewLooter(p, snap)
		cave := tia.Hop(t, e2etest.Spots(t).CaveGate, "greyfang_cave", e2etest.Spots(t).CaveMouth)
		boars := 0
		for _, m := range cave.Mobs {
			if m.Type == "wild_boar" {
				boars++
			}
		}
		if boars == 0 {
			t.Fatalf("no boars in the cave snapshot: %+v", cave.Mobs)
		}
		// Straight back: the arrival gate leads to where she came from.
		tia.Hop(t, e2etest.Spots(t).CaveMouth, "willowmere", e2etest.Spots(t).CaveGate)
		// Each hop checkpointed the target map, so the database and a fresh login both say Willowmere, on the gate.
		var mapID string
		var x, z float64
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := s.DB.QueryRow(context.Background(), `SELECT map_id, pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&mapID, &x, &z); err != nil {
				t.Fatal(err)
			}
			if mapID == "willowmere" && x == e2etest.Spots(t).CaveGate.X && z == e2etest.Spots(t).CaveGate.Z {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if mapID != "willowmere" || x != e2etest.Spots(t).CaveGate.X || z != e2etest.Spots(t).CaveGate.Z {
			t.Fatalf("saved on %s at (%v, %v), want willowmere at (%v, %v)", mapID, x, z, e2etest.Spots(t).CaveGate.X, e2etest.Spots(t).CaveGate.Z)
		}
		_, w := s.Welcome(t, "tia", id)
		if w.GetMapId() != "willowmere" {
			t.Fatalf("welcomed on %s", w.GetMapId())
		}
	})

	t.Run("from-the-north-pass-to-sunreach-and-back", func(t *testing.T) {
		// The north pass leads out of the region to another region's gate, and its way back leads to the pass.
		northPass, southGate := e2etest.Spots(t).NorthPass, e2etest.Spots(t).SouthGate
		token, id := s.CreateCharacter(t, "una", "Una", "warrior")
		s.Place(t, id, "willowmere", gamedata.Point{X: northPass.X - 2, Z: northPass.Z}, 144, 0)
		p, snap := s.Join(t, token, id)
		una := e2etest.NewLooter(p, snap)
		una.Hop(t, northPass, "sunreach", southGate)
		una.Hop(t, southGate, "willowmere", northPass)
	})
}

// From a finga/test flake: Quinn ended her hunt just east of an oak, and the way to the portal west of it ran straight through its trunk. A walk goes round it either way, on the region's paths, never stalling: each leg takes at most 2.5 times its straight line at walking speed, where walking straight into the trunk and jumping takes five.
func TestAWalkGoesRoundATreeInItsWay(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	region, _ := data.Map("willowmere")
	west, east, ok := treeInTheWay(region, data.Movement().Radius)
	if !ok {
		t.Fatal("no tree of Willowmere's stands with open ground 14 m west of it and 2 m east")
	}
	token, id := s.CreateCharacter(t, "oak", "Oak", "warrior")
	s.Place(t, id, "willowmere", west, 144, 0)
	l, _ := s.Hello(t, token, id)
	for _, to := range []gamedata.Point{east, west} {
		from, start := gamedata.Point{X: l.X, Z: l.Z}, l.Tick()
		if !l.WalkTo(to, 30*time.Second) {
			t.Fatalf("stopped at (%.2f, %.2f) on %s, short of %+v", l.X, l.Z, l.MapID, to)
		}
		straight := math.Hypot(to.X-from.X, to.Z-from.Z) / data.Movement().WalkSpeed * float64(l.TickHz)
		if took := float64(l.Tick() - start); took > 2.5*straight {
			t.Fatalf("%+v to %+v took %.0f ticks, over 2.5 times the %.0f a straight walk takes", from, to, took, straight)
		}
		t.Logf("%+v to %+v in %d ticks, %.0f straight", from, to, l.Tick()-start, straight)
	}
}

// treeInTheWay finds the tree of m nearest its start with open ground 2 m east of its trunk and 14 m west of it, a line along z with no other solid on it and no camp's mobs within 40 m of it, so the way between the two runs through the trunk alone.
func treeInTheWay(m *gamedata.Map, radius float64) (west, east gamedata.Point, ok bool) {
	g := m.Geometry
	trees := slices.SortedFunc(slices.Values(g.Cylinders), func(a, b sim.Cylinder) int {
		return cmp.Compare(math.Hypot(a.X-m.PlayerSpawn.X, a.Z-m.PlayerSpawn.Z), math.Hypot(b.X-m.PlayerSpawn.X, b.Z-m.PlayerSpawn.Z))
	})
	for _, c := range trees {
		if c.Radius > 0.5 || c.Top-g.Terrain.Height(c.X, c.Z) < 4 {
			continue // not a trunk
		}
		clear := true
		for x := c.X - 14; x <= c.X+2 && clear; x += 0.5 {
			if math.Abs(x-c.X) > c.Radius+1 {
				clear = m.Grid().Fits(nav.Point{X: x, Z: c.Z}, radius+0.5) && !g.SolidOver(x, c.Z, g.Terrain.Height(x, c.Z)+0.3)
			}
		}
		for _, sp := range m.Spawns {
			clear = clear && math.Hypot(sp.At.X-c.X, sp.At.Z-c.Z) > sp.Radius+40
		}
		if clear {
			return gamedata.Point{X: c.X - 14, Z: c.Z}, gamedata.Point{X: c.X + 2, Z: c.Z}, true
		}
	}
	return gamedata.Point{}, gamedata.Point{}, false
}

// The one row on the shipped data, whose Willowmere is generated whole, 1 km across: every other row runs on the stand-ins. A new character joins at Willowmere's start, a hop through the real cave gate goes down and back, and one placed in the Sunken Shrine joins there.
func TestTheShippedRegionsRunOnTheRealData(t *testing.T) {
	t.Parallel()
	t0 := time.Now()
	phase := func(what string) { fmt.Fprintf(os.Stderr, "E2ETIMING shipped %s at +%v\n", what, time.Since(t0)) }
	s := newStackOver(t, e2etest.RealDataDir())
	phase("stack up")
	data, err := load.Dir(s.Data)
	phase("load.Dir")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := e2etest.LayoutOf(data)
	if err != nil {
		t.Fatal(err)
	}
	phase("layout")
	token, id := s.CreateCharacter(t, "rey", "Rey", "warrior")
	l, w := s.Hello(t, token, id)
	if w.GetMapId() != "willowmere" || math.Hypot(l.X-sp.Start.X, l.Z-sp.Start.Z) > 1 {
		t.Fatalf("a new character joined %s at (%.1f, %.1f); want willowmere at %+v", w.GetMapId(), l.X, l.Z, sp.Start)
	}

	token, id = s.CreateCharacter(t, "kai", "Kai", "warrior")
	s.Place(t, id, "willowmere", gamedata.Point{X: sp.CaveGate.X - 2, Z: sp.CaveGate.Z}, 144, 0)
	p, snap := s.Join(t, token, id)
	kai := e2etest.NewLooter(p, snap)
	phase("kai joined")
	kai.Hop(t, sp.CaveGate, "greyfang_cave", sp.CaveMouth)
	phase("hop in")
	kai.Hop(t, sp.CaveMouth, "willowmere", sp.CaveGate)
	phase("hop out")

	shrine, _ := data.Map("sunken_shrine")
	token, id = s.CreateCharacter(t, "lou", "Lou", "warrior")
	s.Place(t, id, "sunken_shrine", shrine.PlayerSpawn, 144, 0)
	if _, w := s.Hello(t, token, id); w.GetMapId() != "sunken_shrine" {
		t.Fatalf("a character placed in the Sunken Shrine joined %s", w.GetMapId())
	}
	phase("lou joined")
}
