package e2e_test

import (
	"testing"
	"time"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// TestAWorldRestartKeepsSocketsAndRejoins: the world restarts (a deploy, a
// crash) while two players are connected. The gateway keeps both sockets
// open, and once the world is back each player is joined again from the
// last checkpoint and gets a Snapshot; the game goes on.
func TestAWorldRestartKeepsSocketsAndRejoins(t *testing.T) {
	s := newStack(t)
	var ls []*looter
	for _, n := range [][2]string{{"gus", "Gus"}, {"hal", "Hal"}} {
		token, id := s.createCharacter(t, n[0], n[1], "warrior")
		p, snap := s.join(t, token, id)
		ls = append(ls, newLooter(p, snap))
	}
	s.world.restart()
	for _, l := range ls {
		if !l.pump(10*time.Second, func(m wsconn.Message) bool { return m.Type == "Snapshot" }) {
			t.Fatalf("%s got no Snapshot after the restart (closed: %v)", l.id, l.closed(0))
		}
	}
	gus := ls[0]
	if err := gus.trySend(wsconn.Message{Data: protocol.Ping{ClientTime: 1}}); err != nil {
		t.Fatal(err)
	}
	if !gus.pump(5*time.Second, func(m wsconn.Message) bool { return m.Type == "Pong" }) {
		t.Fatal("no Pong: the socket did not survive the restart")
	}
	// The world plays on: Gus walks.
	start := gus.x
	moved := gus.until(5*time.Second, func() bool {
		_ = gus.input(protocol.Input{Right: true})
		gus.pump(50*time.Millisecond, nil)
		return gus.x > start+20
	})
	if !moved {
		t.Fatalf("Gus did not move after the restart (at %.0f from %.0f)", gus.x, start)
	}
}
