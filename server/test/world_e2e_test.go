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
)

// A restart kicks every player with server_restart; each reconnects with its session and is welcomed back with what the game saved.
func TestARestartBringsPlayersBackByReconnecting(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var ls []*looter
	for _, n := range [][2]string{{"gus", "Gus"}, {"hal", "Hal"}} {
		token, id := s.createCharacter(t, n[0], n[1], "warrior")
		p, snap := s.join(t, token, id)
		ls = append(ls, newLooter(p, snap))
	}
	s.give(t, ls[0].id, "red_potion", nil) // the item module's alone: no push reaches the client
	s.restart()
	for k, l := range ls {
		if kicked := l.expect("Kicked").Data.(*clientv1.Kicked); kicked.Reason != clientv1.KickReason_KICK_REASON_SERVER_RESTART {
			t.Fatalf("%s was kicked for %v", l.id, kicked.Reason)
		}
		if !l.closed(5 * time.Second) {
			t.Fatalf("%s's socket outlived the restart", l.id)
		}
		back, w := s.hello(t, l.token, l.id)
		ls[k] = back
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

// SIGTERM drains the game: the players are kicked, and the world saves them well within the shutdown grace.
func TestASigtermedGameSavesItsPlayers(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds and runs the game as its own process")
	}
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGTERM")
	}
	s, game := processStack(t, "../data", "FINGA_SHUTDOWN_GRACE=60s")
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

	if took := game.terminate(90 * time.Second); took > 10*time.Second {
		t.Fatalf("the game took %s to stop", took)
	}
	if kicked := sam.expect("Kicked").Data.(*clientv1.Kicked); kicked.Reason != clientv1.KickReason_KICK_REASON_SERVER_RESTART {
		t.Fatalf("Sam was kicked for %v", kicked.Reason)
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
	if out := game.out.String(); strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("the game logged errors:\n%s", out)
	}
}
