package protocol_test

import (
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/protocol"
)

// A step moves the view held by whole centimetres from where its place rounds to, and says only what changed; the player's own steps are left out.
func TestAnExpanderReadsStepsAgainstTheViewsHeld(t *testing.T) {
	e := protocol.NewExpander("a")
	e.Snapshot(&clientv1.Snapshot{
		Players: []*clientv1.PlayerView{{Id: "a", Slot: 1, X: 10}, {Id: "r", Slot: 2, X: 20.126, Z: 6, Vx: 1, Hp: 50, Name: "Robin"}},
		Mobs:    []*clientv1.MobView{{Id: 9, X: 3.333, Hp: 34, Type: "green_slime"}},
	})
	d := &clientv1.Delta{
		Steps:    []*clientv1.PlayerStep{{Slot: 1, Dx: 5}, {Slot: 2, Dx: -13, Dz: 4, Vx: proto.Int32(2500), Hp: proto.Int32(45)}},
		MobSteps: []*clientv1.MobStep{{Id: 9, Dx: 1, Yaw: proto.Int32(-1813), Hp: proto.Int32(20)}},
	}
	e.Delta(d)
	if len(d.Players) != 1 || d.Steps != nil || d.MobSteps != nil {
		t.Fatalf("%d players, steps %v, mob steps %v; want Robin's step alone, read", len(d.Players), d.Steps, d.MobSteps)
	}
	r := d.Players[0]
	if r.Name != "Robin" || math.Abs(r.X-20) > 1e-9 || math.Abs(r.Z-6.04) > 1e-9 || r.Vx != 2.5 || r.Hp != 45 {
		t.Fatalf("Robin read as %+v", r)
	}
	if m := d.Mobs[0]; m.Type != "green_slime" || math.Abs(m.X-3.34) > 1e-9 || m.Hp != 20 || math.Abs(float64(m.Yaw)+1.813) > 1e-6 {
		t.Fatalf("the slime read as %+v", m)
	}
	e.Delta(&clientv1.Delta{RemovedPlayers: []string{"r"}})
	gone := &clientv1.Delta{Steps: []*clientv1.PlayerStep{{Slot: 2, Dx: 1}}}
	e.Delta(gone)
	if len(gone.Players) != 0 {
		t.Fatalf("a step of a player who left read as %+v", gone.Players)
	}
}

// A body sent both ways in one frame, a step in the cell it left and its whole view in the cell it came to, ends as the whole view says, and is last in the frame's views.
func TestAnExpanderTakesAWholeViewAsAFramesLastWordOnABody(t *testing.T) {
	e := protocol.NewExpander("a")
	e.Snapshot(&clientv1.Snapshot{
		Players: []*clientv1.PlayerView{{Id: "a", Slot: 1}, {Id: "r", Slot: 2, X: 20, Name: "Robin"}},
		Mobs:    []*clientv1.MobView{{Id: 9, X: 3, Type: "green_slime"}},
	})
	d := &clientv1.Delta{
		Players:  []*clientv1.PlayerView{{Id: "r", Slot: 2, X: 20.1, Name: "Robin"}},
		Steps:    []*clientv1.PlayerStep{{Slot: 2, Dx: 10}},
		Mobs:     []*clientv1.MobView{{Id: 9, X: 3.1, Type: "green_slime"}},
		MobSteps: []*clientv1.MobStep{{Id: 9, Dx: 10}},
	}
	e.Delta(d)
	if r := d.Players[len(d.Players)-1]; math.Abs(r.X-20.1) > 1e-9 {
		t.Fatalf("Robin ends at %v; want the whole view's 20.1, not a step past it", r.X)
	}
	if m := d.Mobs[len(d.Mobs)-1]; math.Abs(m.X-3.1) > 1e-9 {
		t.Fatalf("the slime ends at %v; want the whole view's 3.1", m.X)
	}
	next := &clientv1.Delta{Steps: []*clientv1.PlayerStep{{Slot: 2, Dx: 5}}}
	e.Delta(next)
	if r := next.Players[0]; math.Abs(r.X-20.15) > 1e-9 {
		t.Fatalf("Robin's next step reads at %v; want 20.15, from the whole view", r.X)
	}
}

// A whole view of another body, or a mob, is held and returned at the centimetre, as a step leaves it, so it matches the world's kept body; the player's own view stays exact.
func TestAnExpanderHoldsOthersWholeViewsAtTheCentimetre(t *testing.T) {
	e := protocol.NewExpander("a")
	d := &clientv1.Delta{
		Players: []*clientv1.PlayerView{{Id: "a", Slot: 1, X: 10.0049, Z: 5.1234}, {Id: "r", Slot: 2, X: 20.1249, Y: 0.0051, Z: 6.5}},
		Mobs:    []*clientv1.MobView{{Id: 9, X: 3.3349, Z: 4.0051}},
	}
	e.Snapshot(&clientv1.Snapshot{Players: []*clientv1.PlayerView{{Id: "a", Slot: 1}, {Id: "r", Slot: 2}}, Mobs: []*clientv1.MobView{{Id: 9}}})
	e.Delta(d)
	a, r, m := d.Players[0], d.Players[1], d.Mobs[0]
	if a.X != 10.0049 || a.Z != 5.1234 {
		t.Fatalf("the player's own view read as (%v, %v); want it exact", a.X, a.Z)
	}
	if r.X != 20.12 || r.Y != 0.01 || r.Z != 6.5 || m.X != 3.33 || m.Z != 4.01 {
		t.Fatalf("Robin read as (%v, %v, %v) and the slime as (%v, %v); want the centimetre", r.X, r.Y, r.Z, m.X, m.Z)
	}
	step := &clientv1.Delta{Steps: []*clientv1.PlayerStep{{Slot: 2, Dx: 1}}}
	e.Delta(step)
	if got := step.Players[0].X; math.Abs(got-20.13) > 1e-9 {
		t.Fatalf("a step after the whole view put Robin at %v; want 20.13", got)
	}
}
