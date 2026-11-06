package e2e_test

import (
	"context"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

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
	if err := gus.trySend(wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}}); err != nil {
		t.Fatal(err)
	}
	if !gus.pump(5*time.Second, func(m wsconn.Message) bool { return m.Type == "Pong" }) {
		t.Fatal("no Pong: the socket did not survive the restart")
	}
	start := gus.x
	moved := gus.until(5*time.Second, func() bool {
		_ = gus.input(&clientv1.Input{Right: true})
		gus.pump(50*time.Millisecond, nil)
		return gus.x > start+20
	})
	if !moved {
		t.Fatalf("Gus did not move after the restart (at %.0f from %.0f)", gus.x, start)
	}
}

// SIGTERM ends the gateway's subscription at once, so the world saves its players well within its shutdown grace.
func TestASigtermedWorldSavesItsPlayers(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs every service as its own process")
	}
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGTERM")
	}
	s, procs := processStack(t, "FINGA_SHUTDOWN_GRACE=60s")
	token, id := s.createCharacter(t, "sam", "Sam", "warrior")
	p, snap := s.join(t, token, id)
	sam := newLooter(p, snap)
	start := sam.x
	moved := sam.until(5*time.Second, func() bool {
		_ = sam.input(&clientv1.Input{Right: true})
		sam.pump(50*time.Millisecond, nil)
		return sam.x > start+40
	})
	if !moved {
		t.Fatalf("Sam did not move (at %.0f from %.0f)", sam.x, start)
	}
	_ = sam.input(&clientv1.Input{})
	sam.pump(500*time.Millisecond, nil)

	world := procs["world"]
	if took := world.terminate(90 * time.Second); took > 10*time.Second {
		t.Fatalf("the world took %s to stop", took)
	}
	var x float32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.db.QueryRow(ctx, `SELECT pos_x FROM characters WHERE id = $1`, id).Scan(&x); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(x)-sam.x) > 5 {
		t.Fatalf("saved at x=%.0f; Sam stood at %.0f, from %.0f", x, sam.x, start)
	}
	if out := world.out.String(); strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("the world logged errors:\n%s", out)
	}
}
