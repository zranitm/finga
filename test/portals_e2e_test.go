package e2e_test

import (
	"context"
	"testing"
	"time"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// hop walks to x on the current map, enters the portal there, and returns
// the target map's snapshot once MapChange named the expected map and
// arrival point.
func (l *looter) hop(t *testing.T, x float64, wantMap string, wantX float64) protocol.Snapshot {
	t.Helper()
	if !l.walkTo(x, 25*time.Second) {
		t.Fatalf("could not reach x %.0f (at %.0f)", x, l.x)
	}
	hp := l.hp
	if err := l.enterPortal(); err != nil {
		t.Fatal(err)
	}
	var mc protocol.MapChange
	if !l.pump(5*time.Second, func(m wsconn.Message) bool {
		if m.Type != "MapChange" {
			return false
		}
		mc = m.Data.(protocol.MapChange)
		return true
	}) {
		t.Fatalf("no MapChange after EnterPortal at %.0f", l.x)
	}
	if mc.MapID != wantMap || mc.X != wantX || mc.Y != 608 {
		t.Fatalf("map change %+v, want %s at %.0f", mc, wantMap, wantX)
	}
	var snap protocol.Snapshot
	if !l.pump(5*time.Second, func(m wsconn.Message) bool {
		if m.Type != "Snapshot" {
			return false
		}
		snap = m.Data.(protocol.Snapshot)
		return true
	}) {
		t.Fatal("no Snapshot after MapChange")
	}
	if l.x != wantX || l.hp != hp {
		t.Fatalf("after the hop: at %.0f with %d HP; want %.0f with %d HP", l.x, l.hp, wantX, hp)
	}
	return snap
}

func (l *looter) enterPortal() error {
	l.seq++
	return l.trySend(wsconn.Message{Seq: l.seq, Data: protocol.EnterPortal{}})
}

func TestTravel(t *testing.T) {
	s := newStack(t)
	token, id := s.createCharacter(t, "sam", "Sam", "warrior")
	p, snap := s.join(t, token, id)
	l := newLooter(p, snap)
	l.pump(300*time.Millisecond, nil)

	t.Run("B5/the-town-is-safe", func(t *testing.T) {
		if len(snap.Mobs) != 0 {
			t.Fatalf("the town has %d mobs", len(snap.Mobs))
		}
		if err := l.attack(); err != nil {
			t.Fatal(err)
		}
		l.pump(500*time.Millisecond, func(m wsconn.Message) bool {
			if m.Type == "Damage" {
				t.Fatalf("damage in the town: %+v", m.Data)
			}
			return false
		})
	})

	t.Run("B3/off-a-portal-EnterPortal-does-nothing", func(t *testing.T) {
		if err := l.enterPortal(); err != nil {
			t.Fatal(err)
		}
		if l.pump(500*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "MapChange" }) {
			t.Fatal("a map change from the town spawn")
		}
	})

	t.Run("B3-B5/walk-from-the-town-to-the-cave-and-back", func(t *testing.T) {
		// Portal rectangles from the maps: the town's to_forest at x 1200..1232,
		// the forest's from_town at 48..80 and to_cave at 1840..1872, the
		// cave's from_forest at 48..80; arrivals are their bottom centres.
		forest := l.hop(t, 1216, "forest", 64)
		if len(forest.Mobs) == 0 {
			t.Fatal("no mobs in the forest snapshot")
		}
		cave := l.hop(t, 1856, "cave", 64)
		boars := 0
		for _, m := range cave.Mobs {
			if m.Type == "wild_boar" {
				boars++
			}
		}
		if boars == 0 {
			t.Fatalf("no boars in the cave snapshot: %+v", cave.Mobs)
		}
		// Straight back: the arrival portal leads to where she came from.
		l.hop(t, 64, "forest", 1856)
		town := l.hop(t, 64, "town", 1216)
		if len(town.Mobs) != 0 {
			t.Fatal("mobs in the town")
		}
		// Each hop checkpointed the character as already on the target
		// map, so the database and a fresh login both say town.
		var mapID string
		var x float64
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := s.db.QueryRow(context.Background(), `SELECT map_id, pos_x FROM characters WHERE id = $1`, id).Scan(&mapID, &x); err != nil {
				t.Fatal(err)
			}
			if mapID == "town" && x == 1216 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if mapID != "town" || x != 1216 {
			t.Fatalf("saved on %s at %.0f, want town at 1216", mapID, x)
		}
		_, w := s.welcome(t, "sam", id)
		if w.MapID != "town" {
			t.Fatalf("welcomed on %s", w.MapID)
		}
	})
}
