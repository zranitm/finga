package e2e_test

import (
	"context"
	"math"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/gamedata"
	"finga/pkg/wsconn"
)

// hop stands in the portal at at and presses until MapChange takes it to wantMap at want; a press is repeated only after an input sent behind it came back without one.
func (l *looter) hop(t *testing.T, at gamedata.Point, wantMap string, want gamedata.Point) *clientv1.Snapshot {
	t.Helper()
	var mc *clientv1.MapChange
	var hp int32
	presses, next := 0, time.Now()
	for deadline := time.Now().Add(30 * time.Second); mc == nil; {
		if !time.Now().Before(deadline) {
			t.Fatalf("no MapChange after %d presses of EnterPortal at (%.2f, %.2f)", presses, l.x, l.z)
		}
		// Portals are 1.2 m round: within 0.75 m of the middle, standing still on the ground, the player is inside one.
		away := func() bool { return math.Hypot(at.X-l.x, at.Z-l.z) >= 0.75 }
		if away() && !l.walkTo(at, time.Until(deadline)) {
			t.Fatalf("could not reach (%v, %v) (at %.2f, %.2f)", at.X, at.Z, l.x, l.z)
		}
		l.pump(time.Until(next), nil) // the gateway takes 2 presses a second
		if !l.until(2*time.Second, l.standing) || away() {
			continue // still in the air, or knocked out of it while landing
		}
		if err := l.enterPortal(); err != nil {
			t.Fatal(err)
		}
		presses++
		next = time.Now().Add(500 * time.Millisecond)
		if err := l.input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		after := l.seq
		l.pump(time.Until(deadline), func(m wsconn.Message) bool {
			if change, ok := m.Data.(*clientv1.MapChange); ok {
				mc, hp = change, l.hp // as the last frame before it left them: the press is taken before the tick's mob hits
				return true
			}
			return l.ack >= after // applied after the press, with no MapChange: the press missed
		})
	}
	if mc.GetMapId() != wantMap || mc.X != want.X || mc.Y != 0 || mc.Z != want.Z { // every portal the rows take stands on open ground at 0
		t.Fatalf("map change %+v, want %s at (%v, 0, %v)", mc, wantMap, want.X, want.Z)
	}
	var snap *clientv1.Snapshot
	if !l.pump(5*time.Second, func(m wsconn.Message) bool {
		if m.Type != "Snapshot" {
			return false
		}
		snap = m.Data.(*clientv1.Snapshot)
		return true
	}) {
		t.Fatal("no Snapshot after MapChange")
	}
	if l.x != want.X || l.z != want.Z || l.hp != hp {
		t.Fatalf("after the hop: at (%v, %v) with %d HP; want (%v, %v) with %d HP", l.x, l.z, l.hp, want.X, want.Z, hp)
	}
	return snap
}

// standing is the player at rest on the ground, as the last frame shows it.
func (l *looter) standing() bool { return l.onGround && l.vx == 0 && l.vy == 0 && l.vz == 0 }

func (l *looter) enterPortal() error {
	return l.trySend(wsconn.Message{Seq: l.nextSeq(), Data: &clientv1.EnterPortal{}})
}

func TestTravel(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, id := s.createCharacter(t, "sam", "Sam", "warrior")
	p, snap := s.join(t, token, id)
	l := newLooter(p, snap)
	l.pump(300*time.Millisecond, nil)

	t.Run("the-town-is-safe", func(t *testing.T) {
		if len(snap.Mobs) != 0 {
			t.Fatalf("the town has %d mobs", len(snap.Mobs))
		}
		if err := l.attack(0); err != nil {
			t.Fatal(err)
		}
		l.pump(500*time.Millisecond, func(m wsconn.Message) bool {
			if m.Type == "Damage" {
				t.Fatalf("damage in the town: %+v", m.Data)
			}
			return false
		})
	})

	t.Run("off-a-portal-EnterPortal-does-nothing", func(t *testing.T) {
		if err := l.enterPortal(); err != nil {
			t.Fatal(err)
		}
		if l.pump(500*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "MapChange" }) {
			t.Fatal("a map change from the town spawn")
		}
	})

	t.Run("walk-from-the-town-to-the-forest-and-back", func(t *testing.T) {
		// Portal centres, where hops start and arrivals land.
		forest := l.hop(t, townPortal, "forest", forestFromTown)
		if len(forest.Mobs) == 0 {
			t.Fatal("no mobs in the forest snapshot")
		}
		// Straight back: the arrival portal leads to where she came from.
		town := l.hop(t, forestFromTown, "town", townPortal)
		if len(town.Mobs) != 0 {
			t.Fatal("mobs in the town")
		}
		// Each hop checkpointed the target map, so the database and a fresh login both say town.
		var mapID string
		var x, z float64
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := s.db.QueryRow(context.Background(), `SELECT map_id, pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&mapID, &x, &z); err != nil {
				t.Fatal(err)
			}
			if mapID == "town" && x == townPortal.X && z == townPortal.Z {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if mapID != "town" || x != townPortal.X || z != townPortal.Z {
			t.Fatalf("saved on %s at (%v, %v), want town at (%v, %v)", mapID, x, z, townPortal.X, townPortal.Z)
		}
		_, w := s.welcome(t, "sam", id)
		if w.GetMapId() != "town" {
			t.Fatalf("welcomed on %s", w.GetMapId())
		}
	})

	t.Run("from-the-forest-to-the-cave-and-back", func(t *testing.T) {
		// She starts on the forest's to_cave portal, as a walk across the forest would end.
		token, id := s.createCharacter(t, "tia", "Tia", "warrior")
		s.place(t, id, "forest", forestToCave, 144, 0)
		p, snap := s.join(t, token, id)
		tia := newLooter(p, snap)
		cave := tia.hop(t, forestToCave, "cave", caveFromForest)
		boars := 0
		for _, m := range cave.Mobs {
			if m.Type == "wild_boar" {
				boars++
			}
		}
		if boars == 0 {
			t.Fatalf("no boars in the cave snapshot: %+v", cave.Mobs)
		}
		tia.hop(t, caveFromForest, "forest", forestToCave)
	})
}
