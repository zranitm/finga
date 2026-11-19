package e2e_test

import (
	"context"
	"math"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestTravel(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, id := s.CreateCharacter(t, "sam", "Sam", "warrior")
	p, snap := s.Join(t, token, id)
	l := e2etest.NewLooter(p, snap)
	l.Pump(300*time.Millisecond, nil)

	t.Run("the-town-is-safe", func(t *testing.T) {
		if len(snap.Mobs) != 0 {
			t.Fatalf("the town has %d mobs", len(snap.Mobs))
		}
		if err := l.Attack(0); err != nil {
			t.Fatal(err)
		}
		l.Pump(500*time.Millisecond, func(m wsconn.Message) bool {
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
		if l.Pump(500*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "MapChange" }) {
			t.Fatal("a map change from the town spawn")
		}
	})

	t.Run("walk-from-the-town-to-the-forest-and-back", func(t *testing.T) {
		// Portal centres, where hops start and arrivals land.
		forest := l.Hop(t, e2etest.TownPortal, "forest", e2etest.ForestFromTown)
		if len(forest.Mobs) == 0 {
			t.Fatal("no mobs in the forest snapshot")
		}
		// Straight back: the arrival portal leads to where she came from.
		town := l.Hop(t, e2etest.ForestFromTown, "town", e2etest.TownPortal)
		if len(town.Mobs) != 0 {
			t.Fatal("mobs in the town")
		}
		// Each hop checkpointed the target map, so the database and a fresh login both say town.
		var mapID string
		var x, z float64
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := s.DB.QueryRow(context.Background(), `SELECT map_id, pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&mapID, &x, &z); err != nil {
				t.Fatal(err)
			}
			if mapID == "town" && x == e2etest.TownPortal.X && z == e2etest.TownPortal.Z {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if mapID != "town" || x != e2etest.TownPortal.X || z != e2etest.TownPortal.Z {
			t.Fatalf("saved on %s at (%v, %v), want town at (%v, %v)", mapID, x, z, e2etest.TownPortal.X, e2etest.TownPortal.Z)
		}
		_, w := s.Welcome(t, "sam", id)
		if w.GetMapId() != "town" {
			t.Fatalf("welcomed on %s", w.GetMapId())
		}
	})

	t.Run("from-the-forest-to-the-cave-and-back", func(t *testing.T) {
		// She starts on the forest's to_cave portal, as a walk across the forest would end.
		token, id := s.CreateCharacter(t, "tia", "Tia", "warrior")
		s.Place(t, id, "forest", e2etest.ForestToCave, 144, 0)
		p, snap := s.Join(t, token, id)
		tia := e2etest.NewLooter(p, snap)
		cave := tia.Hop(t, e2etest.ForestToCave, "cave", e2etest.CaveFromForest)
		boars := 0
		for _, m := range cave.Mobs {
			if m.Type == "wild_boar" {
				boars++
			}
		}
		if boars == 0 {
			t.Fatalf("no boars in the cave snapshot: %+v", cave.Mobs)
		}
		tia.Hop(t, e2etest.CaveFromForest, "forest", e2etest.ForestToCave)
	})
}

// From a finga/test flake: Quinn ended her hunt just east of the forest's oak at (16, 20), and the way to the town portal at (2, 20) runs straight through its trunk. A walk goes round it either way, on the map the hop took it to, never stalling: each leg takes at most half again its straight line at walking speed.
func TestAWalkGoesRoundATreeInItsWay(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	token, id := s.CreateCharacter(t, "oak", "Oak", "warrior")
	if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET pos_x = $2, pos_z = $3 WHERE id = $1`, id, e2etest.TownPortal.X-2, e2etest.TownPortal.Z); err != nil { // by the portal: the walk there isn't what's tested
		t.Fatal(err)
	}
	l, _ := s.Hello(t, token, id)
	l.Hop(t, e2etest.TownPortal, "forest", e2etest.ForestFromTown)
	if l.MapID != "forest" {
		t.Fatalf("after the hop the looter walks on %q, want the forest's paths", l.MapID)
	}
	eastOfTheOak := gamedata.Point{X: 18, Z: 20}
	for _, to := range []gamedata.Point{eastOfTheOak, e2etest.ForestFromTown} {
		from, start := gamedata.Point{X: l.X, Z: l.Z}, l.Tick()
		if !l.WalkTo(to, 30*time.Second) {
			t.Fatalf("stopped at (%.2f, %.2f) on %s, short of %+v", l.X, l.Z, l.MapID, to)
		}
		straight := math.Hypot(to.X-from.X, to.Z-from.Z) / data.Movement().WalkSpeed * float64(l.TickHz)
		if took := float64(l.Tick() - start); took > 1.5*straight {
			t.Fatalf("%+v to %+v took %.0f ticks, over half again the %.0f a straight walk takes", from, to, took, straight)
		}
		t.Logf("%+v to %+v in %d ticks, %.0f straight", from, to, l.Tick()-start, straight)
	}
}
