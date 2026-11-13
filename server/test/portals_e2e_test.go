package e2e_test

import (
	"context"
	"math"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// hop takes the portal at x to wantMap, arriving at wantX, and returns the new map's snapshot. It goes as a player does: it stands still inside the portal and presses, and presses again when a press didn't take, since the world drops one unanswered from a player in the air, outside the portal or with an item call in flight. An input sent right after a press is applied only once the press was handled, so a press is repeated only when its input came back without a MapChange, never while one may still be on its way to take the player back through the portal they arrive in.
func (l *looter) hop(t *testing.T, x float64, wantMap string, wantX float64) *clientv1.Snapshot {
	t.Helper()
	var mc *clientv1.MapChange
	var hp int32
	presses, next := 0, time.Now()
	for deadline := time.Now().Add(30 * time.Second); mc == nil; {
		if !time.Now().Before(deadline) {
			t.Fatalf("no MapChange after %d presses of EnterPortal at %.0f", presses, l.x)
		}
		// Portals are 32 px wide: within 12 px of the middle, standing still on the ground, the player is inside one.
		if math.Abs(x-l.x) >= 12 && !l.walkTo(x, time.Until(deadline)) {
			t.Fatalf("could not reach x %.0f (at %.0f)", x, l.x)
		}
		l.pump(time.Until(next), nil) // the gateway takes 2 presses a second
		if !l.until(2*time.Second, l.standing) || math.Abs(x-l.x) >= 12 {
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
				mc, hp = change, l.hp // as the last frame before it left them: the press is taken before the tick's contact damage
				return true
			}
			return l.ack >= after // applied after the press, with no MapChange: the press missed
		})
	}
	if mc.GetMapId() != wantMap || mc.X != wantX || mc.Y != 608 {
		t.Fatalf("map change %+v, want %s at %.0f", mc, wantMap, wantX)
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
	if l.x != wantX || l.hp != hp {
		t.Fatalf("after the hop: at %.0f with %d HP; want %.0f with %d HP", l.x, l.hp, wantX, hp)
	}
	return snap
}

// standing is the player at rest on the ground, as the last frame shows it.
func (l *looter) standing() bool { return l.onGround && l.vx == 0 && l.vy == 0 }

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

	t.Run("off-a-portal-EnterPortal-does-nothing", func(t *testing.T) {
		if err := l.enterPortal(); err != nil {
			t.Fatal(err)
		}
		if l.pump(500*time.Millisecond, func(m wsconn.Message) bool { return m.Type == "MapChange" }) {
			t.Fatal("a map change from the town spawn")
		}
	})

	t.Run("walk-from-the-town-to-the-forest-and-back", func(t *testing.T) {
		// Portal centres, where hops start and arrivals land: town to_forest 1216, forest 64.
		forest := l.hop(t, 1216, "forest", 64)
		if len(forest.Mobs) == 0 {
			t.Fatal("no mobs in the forest snapshot")
		}
		// Straight back: the arrival portal leads to where she came from.
		town := l.hop(t, 64, "town", 1216)
		if len(town.Mobs) != 0 {
			t.Fatal("mobs in the town")
		}
		// Each hop checkpointed the target map, so the database and a fresh login both say town.
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
		if w.GetMapId() != "town" {
			t.Fatalf("welcomed on %s", w.GetMapId())
		}
	})

	t.Run("from-the-forest-to-the-cave-and-back", func(t *testing.T) {
		// Forest to_cave 1856, cave from_forest 64: she starts on the portal, as a walk across the forest would end.
		token, id := s.createCharacter(t, "tia", "Tia", "warrior")
		s.place(t, id, "forest", 1856, 608, 144, 0)
		p, snap := s.join(t, token, id)
		tia := newLooter(p, snap)
		cave := tia.hop(t, 1856, "cave", 64)
		boars := 0
		for _, m := range cave.Mobs {
			if m.Type == "wild_boar" {
				boars++
			}
		}
		if boars == 0 {
			t.Fatalf("no boars in the cave snapshot: %+v", cave.Mobs)
		}
		tia.hop(t, 64, "forest", 1856)
	})
}
