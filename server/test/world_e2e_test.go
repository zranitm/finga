package e2e_test

import (
	"context"
	"maps"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

func TestAWorldRestartKeepsSocketsAndRejoins(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var ls []*looter
	for _, n := range [][2]string{{"gus", "Gus"}, {"hal", "Hal"}} {
		token, id := s.createCharacter(t, n[0], n[1], "warrior")
		p, snap := s.join(t, token, id)
		ls = append(ls, newLooter(p, snap))
	}
	s.give(t, ls[0].id, "red_potion", nil) // the item service's alone: no push reaches the client
	s.world.restart()
	for _, l := range ls {
		var w *clientv1.Welcome
		if !l.pump(comeBack, func(m wsconn.Message) bool {
			if v, ok := m.Data.(*clientv1.Welcome); ok {
				w = v
			}
			return m.Type == "Snapshot"
		}) {
			t.Fatalf("%s got no Snapshot after the restart (closed: %v)", l.id, l.closed(0))
		}
		// The rejoin's Welcome, with no token, shows what the services hold, the potion no push told of included.
		if w == nil || w.GetToken() != "" {
			t.Fatalf("%s got %v ahead of its Snapshot, want a Welcome with no token", l.id, w)
		}
		held := map[string]int{}
		for _, it := range w.GetInventory() {
			held[it.GetTemplate()] += int(it.GetQuantity())
		}
		if want := s.inventoryTotals(t, l.id); !maps.Equal(held, want) {
			t.Fatalf("%s is welcomed back with %v, the database holds %v", l.id, held, want)
		}
		var level int
		var exp int64
		if err := s.db.QueryRow(context.Background(), `SELECT level, exp FROM characters WHERE id = $1`, l.id).Scan(&level, &exp); err != nil {
			t.Fatal(err)
		}
		if c := w.GetCharacter(); int(c.GetLevel()) != level || c.GetExp() != exp {
			t.Fatalf("%s is welcomed back at level %d with %d EXP, the database holds %d and %d", l.id, c.GetLevel(), c.GetExp(), level, exp)
		}
	}
	if held := s.inventoryTotals(t, ls[0].id); held["red_potion"] == 0 {
		t.Fatalf("%s holds %v; want the potion given", ls[0].id, held)
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
	t.Parallel()
	if testing.Short() {
		t.Skip("builds and runs every service as its own process")
	}
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGTERM")
	}
	s, procs := processStack(t, "../data", "FINGA_SHUTDOWN_GRACE=60s")
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
	if !sam.until(10*time.Second, sam.standing) { // where the save must put him, however late the frames come
		t.Fatalf("Sam never stood still after the stop (at %.0f)", sam.x)
	}

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
