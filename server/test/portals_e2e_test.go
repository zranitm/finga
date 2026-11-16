package e2e_test

import (
	"context"
	"testing"
	"time"

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
