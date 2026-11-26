package e2e_test

import (
	"math"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/protocol"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestWalkTogether(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	walk := data.Movement().WalkSpeed
	// A region isn't a town map, so each walks at the walk speed a move speed raises.
	enter := func(username, name, class string) (*e2etest.Player, float64) {
		token, id := s.CreateCharacter(t, username, name, class)
		p := s.Connect(t, id)
		w, _ := p.Hello(token)
		return p, float64(walk * sim.SpeedFactor(w.GetCharacter().GetSheet().GetDerived().GetMoveSpeed()))
	}
	ayla, aylaWalk := enter("alice", "Ayla", "warrior")
	robin, robinWalk := enter("bob", "Robin", "bowman")

	t.Run("M1/both-join-the-town-and-see-each-other", func(t *testing.T) {
		// Robin's snapshot already carried Ayla; Ayla learns of Robin from a delta.
		if _, ok := ayla.Position(robin.ID, 3*time.Second); !ok {
			t.Fatal("Ayla never saw Robin join")
		}
	})

	t.Run("each-sees-the-other-move", func(t *testing.T) {
		ayla.Drain(300 * time.Millisecond) // land and settle
		robin.Drain(300 * time.Millisecond)
		ayla.Send(wsconn.Message{Seq: ayla.NextSeq(), Data: &clientv1.Input{Right: true}})
		v1, ok := robin.Moving(ayla.ID, 3*time.Second)
		if !ok {
			t.Fatal("Robin never saw Ayla move")
		}
		v2, ok := robin.Position(ayla.ID, 3*time.Second)
		if !ok || v2.X <= v1.X || math.Abs(v2.GetVx()-aylaWalk) > 1e-9 || v2.GetHx() != 1 || v2.GetHz() != 0 {
			t.Fatalf("Ayla as Robin sees her: %+v then %+v", v1, v2)
		}
		ayla.Send(wsconn.Message{Seq: ayla.NextSeq(), Data: &clientv1.Input{}})
		robin.Send(wsconn.Message{Seq: robin.NextSeq(), Data: &clientv1.Input{Left: true}})
		r1, ok := ayla.Moving(robin.ID, 3*time.Second)
		if !ok || math.Abs(r1.GetVx()+robinWalk) > 1e-9 || r1.GetHx() != -1 || r1.GetHz() != 0 {
			t.Fatalf("Robin as Ayla sees him: %+v", r1)
		}
		robin.Send(wsconn.Message{Seq: robin.NextSeq(), Data: &clientv1.Input{}})
	})

	t.Run("faster-inputs-do-not-move-faster", func(t *testing.T) {
		ayla.Drain(400 * time.Millisecond)
		robin.Drain(400 * time.Millisecond)
		// For 20 ticks Ayla sends one input a tick and Robin ten; neither moves more than a sprint's step a tick it ran, the fastest a 2 s walk legally goes.
		type seen struct {
			x    float64
			tick uint64
		}
		at := func(p *e2etest.Player, of string, d time.Duration) (seen, bool) {
			v, tick, ok := p.PositionAt(of, d)
			return seen{v.GetX(), tick}, ok
		}
		var startA, startR seen
		for i := range 20 {
			ayla.Send(wsconn.Message{Seq: ayla.NextSeq(), Data: &clientv1.Input{Right: true}})
			for range 10 {
				robin.Send(wsconn.Message{Seq: robin.NextSeq(), Data: &clientv1.Input{Right: true}})
			}
			if i == 0 {
				var ok bool
				if startA, ok = at(robin, ayla.ID, 3*time.Second); !ok {
					t.Fatal("no start position for Ayla")
				}
				if startR, ok = at(ayla, robin.ID, 3*time.Second); !ok {
					t.Fatal("no start position for Robin")
				}
			}
			ayla.Reach(ayla.Tick() + 1) // a tick
		}
		ayla.Send(wsconn.Message{Seq: ayla.NextSeq(), Data: &clientv1.Input{}})
		robin.Send(wsconn.Message{Seq: robin.NextSeq(), Data: &clientv1.Input{}})
		// The stop is each state's last change, so the last delta carrying a character is its end position.
		endA, endR := startA, startR
		for v, ok := at(robin, ayla.ID, 500*time.Millisecond); ok; v, ok = at(robin, ayla.ID, 500*time.Millisecond) {
			endA = v
		}
		for v, ok := at(ayla, robin.ID, 500*time.Millisecond); ok; v, ok = at(ayla, robin.ID, 500*time.Millisecond) {
			endR = v
		}
		step := float64(walk*data.Movement().Sprint.Speed) / float64(s.Cfg.World.TickRateHz)
		for _, w := range []struct {
			name     string
			from, to seen
		}{{"Ayla", startA, endA}, {"Robin", startR, endR}} {
			d, ticks := w.to.x-w.from.x, w.to.tick-w.from.tick
			if d <= 3.125 {
				t.Fatalf("%s walked too little: %.2f m", w.name, d)
			}
			if d > float64(ticks+1)*step { // a step's margin
				t.Fatalf("%s moved %.2f m in %d ticks, more than a sprint's %.2f m a tick", w.name, d, ticks, step)
			}
		}
	})

	t.Run("M1/leaving-is-seen", func(t *testing.T) {
		_ = robin.Conn.Close(websocket.StatusNormalClosure, "bye")
		game := ayla.Game(3 * time.Second)
		for {
			m, ok := game.Next()
			if !ok {
				break
			}
			if m.Type == "Delta" {
				if slices.Contains(m.Data.(*clientv1.Delta).RemovedPlayers, robin.ID) {
					return
				}
			}
		}
		t.Fatal("Ayla never saw Robin leave")
	})
}

func TestSecondLoginKicksTheFirstEndToEnd(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	first := s.Enter(t, "carol", "Cass", "warrior")
	token := s.Call("POST", "/api/sessions", "", map[string]string{"username": "carol", "password": "correct horse"}).Body["token"].(string)
	second := s.Connect(t, first.ID)
	second.Send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: first.ID}})
	second.Expect("Snapshot")
	m := first.Expect("Kicked")
	if m.Data.(*clientv1.Kicked).Reason != clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE {
		t.Fatalf("kicked %+v", m.Data)
	}
	if !first.Closed(3 * time.Second) {
		t.Fatal("first connection still open after Kicked")
	}
	// The old token is revoked too (one active session per account).
	s.MustStatus(s.Call("GET", "/api/characters", first.ID, nil), http.StatusUnauthorized)
}
