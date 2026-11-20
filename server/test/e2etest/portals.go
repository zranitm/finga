package e2etest

import (
	"math"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
)

// Hop stands in the portal at at and presses until MapChange takes it to wantMap at want, for up to 30 s of the world's ticks; a press is repeated only after an input sent behind it came back without one.
func (l *Looter) Hop(t *testing.T, at gamedata.Point, wantMap string, want gamedata.Point) *clientv1.Snapshot {
	t.Helper()
	var mc *clientv1.MapChange
	var hp int32
	presses, next, game := 0, time.Now(), l.Game(30*time.Second)
	for mc == nil {
		if !game.Left() {
			t.Fatalf("no MapChange after %d presses of EnterPortal at (%.2f, %.2f)", presses, l.X, l.Z)
		}
		// Portals are 1.2 m round: within 0.75 m of the middle, standing still on the ground, the player is inside one.
		away := func() bool { return math.Hypot(at.X-l.X, at.Z-l.Z) >= 0.75 }
		if away() && !l.WalkTo(at, game.Remaining()) {
			t.Fatalf("could not reach (%v, %v) (at %.2f, %.2f)", at.X, at.Z, l.X, l.Z)
		}
		l.Pump(time.Until(next), nil) // the gateway takes 2 presses a second, by the wall clock
		if !l.Until(2*time.Second, l.Standing) || away() {
			continue // still in the air, or knocked out of it while landing
		}
		if err := l.EnterPortal(); err != nil {
			t.Fatal(err)
		}
		presses++
		next = time.Now().Add(500 * time.Millisecond)
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		after := l.Seq
		l.During(game.Remaining(), func(m wsconn.Message) bool {
			if change, ok := m.Data.(*clientv1.MapChange); ok {
				mc, hp = change, l.HP // as the last frame before it left them: the press is taken before the tick's mob hits
				return true
			}
			return l.ack >= after // applied after the press, with no MapChange: the press missed
		}, nil, nil)
	}
	if mc.GetMapId() != wantMap || mc.X != want.X || mc.Y != 0 || mc.Z != want.Z { // every portal the rows take stands on open ground at 0
		t.Fatalf("map change %+v, want %s at (%v, 0, %v)", mc, wantMap, want.X, want.Z)
	}
	var snap *clientv1.Snapshot
	if !l.Pump(5*time.Second, func(m wsconn.Message) bool {
		if m.Type != "Snapshot" {
			return false
		}
		snap = m.Data.(*clientv1.Snapshot)
		return true
	}) {
		t.Fatal("no Snapshot after MapChange")
	}
	if l.X != want.X || l.Z != want.Z || l.HP != hp {
		t.Fatalf("after the hop: at (%v, %v) with %d HP; want (%v, %v) with %d HP", l.X, l.Z, l.HP, want.X, want.Z, hp)
	}
	return snap
}

// Standing is the player at rest on the ground, as the last frame shows it.
func (l *Looter) Standing() bool { return l.onGround && l.vx == 0 && l.vy == 0 && l.vz == 0 }

// EnterPortal asks to go through the portal the looter stands in.
func (l *Looter) EnterPortal() error {
	return l.trySend(wsconn.Message{Seq: l.NextSeq(), Data: &clientv1.EnterPortal{}})
}

// Teleport asks for a teleport to mapID, pressed during the last input sent, and returns its Response.
func (l *Looter) Teleport(t *testing.T, req, mapID string) *clientv1.Response {
	t.Helper()
	if err := l.trySend(wsconn.Message{ReqID: req, Seq: l.Seq, Data: &clientv1.Teleport{MapId: mapID}}); err != nil {
		t.Fatal(err)
	}
	return Take(t, l, "Response "+req, &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == req }).R
}
