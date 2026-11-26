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
	"finga/test/e2etest"
)

// A restart kicks every player with server_restart; each reconnects with its session and is welcomed back with what the game saved.
func TestARestartBringsPlayersBackByReconnecting(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var ls []*e2etest.Looter
	for _, n := range [][2]string{{"gus", "Gus"}, {"hal", "Hal"}} {
		token, id := s.CreateCharacter(t, n[0], n[1], "warrior")
		p, snap := s.Join(t, token, id)
		ls = append(ls, e2etest.NewLooter(p, snap))
	}
	s.Give(t, ls[0].ID, "red_potion", nil) // the item module's alone: no push reaches the client
	s.Restart()
	for k, l := range ls {
		if kicked := l.Expect("Kicked").Data.(*clientv1.Kicked); kicked.Reason != clientv1.KickReason_KICK_REASON_SERVER_RESTART {
			t.Fatalf("%s was kicked for %v", l.ID, kicked.Reason)
		}
		if !l.Closed(5 * time.Second) {
			t.Fatalf("%s's socket outlived the restart", l.ID)
		}
		back, w := s.Hello(t, l.Token, l.ID)
		ls[k] = back
		held := map[string]int{}
		for _, it := range w.GetInventory() {
			held[it.GetTemplate()] += int(it.GetQuantity())
		}
		if want := s.InventoryTotals(t, l.ID); !maps.Equal(held, want) {
			t.Fatalf("%s is welcomed back with %v, the database holds %v", l.ID, held, want)
		}
		var level int
		var exp int64
		if err := s.DB.QueryRow(context.Background(), `SELECT level, exp FROM characters WHERE id = $1`, l.ID).Scan(&level, &exp); err != nil {
			t.Fatal(err)
		}
		if c := w.GetCharacter(); int(c.GetLevel()) != level || c.GetExp() != exp {
			t.Fatalf("%s is welcomed back at level %d with %d EXP, the database holds %d and %d", l.ID, c.GetLevel(), c.GetExp(), level, exp)
		}
	}
	if held := s.InventoryTotals(t, ls[0].ID); held["red_potion"] == 0 {
		t.Fatalf("%s holds %v; want the potion given", ls[0].ID, held)
	}
	gus := ls[0]
	start := gus.X
	moved := gus.Until(5*time.Second, func() bool {
		_ = gus.Input(&clientv1.Input{Right: true})
		gus.Pump(50*time.Millisecond, nil)
		return gus.X > start+0.625
	})
	if !moved {
		t.Fatalf("Gus did not move after the restart (at %.2f from %.2f)", gus.X, start)
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
	s, game := processStack(t, e2etest.GameData(t), "FINGA_SHUTDOWN_GRACE=60s")
	token, id := s.CreateCharacter(t, "sam", "Sam", "warrior")
	p, snap := s.Join(t, token, id)
	sam := e2etest.NewLooter(p, snap)
	start := sam.X
	moved := sam.Until(5*time.Second, func() bool {
		_ = sam.Input(&clientv1.Input{Right: true})
		sam.Pump(50*time.Millisecond, nil)
		return sam.X > start+1.25
	})
	if !moved {
		t.Fatalf("Sam did not move (at %.2f from %.2f)", sam.X, start)
	}
	_ = sam.Input(&clientv1.Input{})
	if !sam.Until(10*time.Second, sam.Standing) { // where the save must put him, however late the frames come
		t.Fatalf("Sam never stood still after the stop (at %.2f)", sam.X)
	}

	if took := game.terminate(90 * time.Second); took > 10*time.Second {
		t.Fatalf("the game took %s to stop", took)
	}
	if kicked := sam.Expect("Kicked").Data.(*clientv1.Kicked); kicked.Reason != clientv1.KickReason_KICK_REASON_SERVER_RESTART {
		t.Fatalf("Sam was kicked for %v", kicked.Reason)
	}
	var x, z float32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.DB.QueryRow(ctx, `SELECT pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&x, &z); err != nil {
		t.Fatal(err)
	}
	if math.Hypot(float64(x)-sam.X, float64(z)-sam.Z) > 0.15 {
		t.Fatalf("saved at (%.2f, %.2f); Sam stood at (%.2f, %.2f), from x %.2f", x, z, sam.X, sam.Z, start)
	}
	if out := game.out.String(); strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("the game logged errors:\n%s", out)
	}
}
