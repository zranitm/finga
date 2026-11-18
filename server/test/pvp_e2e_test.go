package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// I1/I2: a player queues for 1v1, meets a bot, accepts, fights its one round to the end, hears the outcome with their rating, and goes back to where they queued with the cooldown they had; their rating row has moved. Rounds are short here, and every wait is for a message, not a time.
func TestADuelAgainstABotRunsToItsEnd(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t,
		e2etest.DataEdit{File: "pvp.yaml", Old: "queue: {bots_after: 15s", New: "queue: {bots_after: 1s"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {prep: 10s, countdown: 3s, between: 5s}", New: "rounds: {prep: 1s, countdown: 1s, between: 1s}"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "result: 10s", New: "result: 1s"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {best_of: 3, most: 5, length: 60s}", New: "rounds: {best_of: 1, most: 1, length: 8s}"},
	))
	token, id := s.CreateCharacter(t, "duelist", "Duelist", "warrior")
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, `INSERT INTO character_skill_timers (character_id, skill_id, kind, until, points, created_at, updated_at) VALUES ($1, 'war_cry', 'cooldown', now() + interval '1 hour', 0, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	p, snap := s.Join(t, token, id)
	l := e2etest.NewLooter(p, snap)
	var home *clientv1.PlayerView
	if !l.Pump(5*time.Second, func(m wsconn.Message) bool {
		if v := ownView(m, id); v != nil {
			home = v
		}
		return home != nil
	}) {
		t.Fatal("no view of the player before queueing")
	}

	ask := func(req string, msg any) {
		t.Helper()
		l.Send(wsconn.Message{ReqID: req, Seq: l.Seq, Data: msg})
		var r *clientv1.Response
		if !l.Pump(5*time.Second, func(m wsconn.Message) bool {
			r, _ = m.Data.(*clientv1.Response)
			return r != nil && m.ReqID == req
		}) || !r.GetOk() {
			t.Fatalf("%s: %+v", req, r)
		}
	}
	ask("queue", &clientv1.PvpQueue{Mode: "duel"})
	var offer *clientv1.PvpOffer
	if !l.Pump(10*time.Second, func(m wsconn.Message) bool { offer, _ = m.Data.(*clientv1.PvpOffer); return offer != nil }) || offer.GetBots() != 1 {
		t.Fatalf("offer %+v, want one with a bot", offer)
	}
	ask("accept", &clientv1.PvpAnswer{Offer: offer.GetOffer(), Accept: true})

	var into *clientv1.MapChange
	if !l.Pump(10*time.Second, func(m wsconn.Message) bool { into, _ = m.Data.(*clientv1.MapChange); return into != nil }) || into.GetMapId() != "ring" {
		t.Fatalf("map change %+v, want into the ring", into)
	}
	var phases []clientv1.MatchPhase
	var ended *clientv1.MatchEnded
	var kit *clientv1.StatsChanged
	if !l.Pump(60*time.Second, func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.MatchState:
			phases = append(phases, d.GetPhase())
		case *clientv1.StatsChanged:
			if kit == nil && d.GetTimers() != nil {
				kit = d
			}
		case *clientv1.MatchEnded:
			ended = d
		}
		return ended != nil
	}) {
		t.Fatalf("no outcome; phases %v", phases)
	}
	t.Logf("phases %v, outcome %v", phases, ended.GetResult())
	if kit == nil || len(kit.GetTimers().GetTimers()) != 0 || kit.GetSheet() == nil {
		t.Fatalf("the sheet in the match %+v, want the kit's with no timers", kit)
	}
	if len(phases) < 3 || phases[0] != clientv1.MatchPhase_MATCH_PHASE_PREP || phases[len(phases)-1] != clientv1.MatchPhase_MATCH_PHASE_RESULT {
		t.Fatalf("phases %v, want prep through to the result", phases)
	}
	if r := ended.GetRating(); r == nil || r.GetBefore() != 1000 || r.GetAfter() == r.GetBefore() || r.GetPlacementLeft() != 4 {
		t.Fatalf("rating change %+v", ended.GetRating())
	}

	var back *clientv1.MapChange
	var world *clientv1.StatsChanged
	if !l.Pump(15*time.Second, func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.MapChange:
			back = d
		case *clientv1.StatsChanged:
			if back != nil && d.GetTimers() != nil {
				world = d
			}
		}
		return world != nil
	}) {
		t.Fatalf("no way back: map change %+v", back)
	}
	if back.GetMapId() != "town" || back.GetX() != home.GetX() || back.GetZ() != home.GetZ() {
		t.Fatalf("back at %+v, want town at (%v, %v)", back, home.GetX(), home.GetZ())
	}
	if ts := world.GetTimers().GetTimers(); len(ts) != 1 || ts[0].GetSkill() != "war_cry" || ts[0].GetLeftMs() < 59*60*1000 {
		t.Fatalf("timers back home %+v, want war_cry's cooldown running on", ts)
	}
	var matches, wins, losses, draws int
	if err := s.DB.QueryRow(ctx, `SELECT matches, wins, losses, draws FROM pvp_ratings WHERE character_id = $1 AND mode = 'duel'`, id).Scan(&matches, &wins, &losses, &draws); err != nil {
		t.Fatal(err)
	}
	if matches != 1 || wins+losses+draws != 1 {
		t.Fatalf("rating row: %d matches, %d-%d-%d", matches, wins, losses, draws)
	}
}

// ownView is the player's own view in a Snapshot or a Delta, nil when it has none.
func ownView(m wsconn.Message, id string) *clientv1.PlayerView {
	var views []*clientv1.PlayerView
	switch d := m.Data.(type) {
	case *clientv1.Snapshot:
		views = d.GetPlayers()
	case *clientv1.Delta:
		views = d.GetPlayers()
	}
	for _, v := range views {
		if v.GetId() == id {
			return v
		}
	}
	return nil
}
