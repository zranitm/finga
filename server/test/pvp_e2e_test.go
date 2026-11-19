package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// I1/I2: a player queues for 1v1, meets a bot, accepts, fights its one round to the end, hears the outcome with their rating, and goes back to where they queued with the cooldown they had; their rating row has moved. Rounds are short here; every wait is for a message, a minute its bound, and finds one that came early.
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
	h := &heard{l: e2etest.NewLooter(p, snap)}
	k := h.since(0, 5*time.Second, func(m wsconn.Message) bool { return ownView(m, id) != nil })
	if k < 0 {
		t.Fatal("no view of the player before queueing")
	}
	home := ownView(h.got[k], id)

	// Each wait reads on from its request, the replies in any order, so one that comes early isn't missed.
	ask := func(req string, msg any) int {
		t.Helper()
		from := len(h.got)
		h.l.Send(wsconn.Message{ReqID: req, Seq: h.l.Seq, Data: msg})
		k := h.since(from, time.Minute, func(m wsconn.Message) bool { return m.Type == "Response" && m.ReqID == req })
		if k < 0 || !h.got[k].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("%s: no ok Response", req)
		}
		return from
	}
	from := ask("queue", &clientv1.PvpQueue{Mode: "duel"})
	k = h.since(from, time.Minute, func(m wsconn.Message) bool { return m.Type == "PvpOffer" || m.Type == "PvpUnqueued" })
	if k < 0 || h.got[k].Type != "PvpOffer" || h.got[k].Data.(*clientv1.PvpOffer).GetBots() != 1 {
		t.Fatalf("after queueing: %+v, want an offer with a bot", h.message(k))
	}
	offer := h.got[k].Data.(*clientv1.PvpOffer)

	sent := time.Now()
	from = ask("accept", &clientv1.PvpAnswer{Offer: offer.GetOffer(), Accept: true})
	answered := time.Since(sent)
	into := h.since(from, time.Minute, func(m wsconn.Message) bool {
		return m.Type == "MapChange" || m.Type == "PvpOfferCancelled" || m.Type == "PvpUnqueued"
	})
	if into < 0 || h.got[into].Type != "MapChange" || h.got[into].Data.(*clientv1.MapChange).GetMapId() != "ring" {
		t.Fatalf("after accepting: %+v, want a map change into the ring", h.message(into))
	}
	t.Logf("accepted: the Response after %v, the map change after %v, %s", answered, time.Since(sent), h.order(from, into, "accept"))

	ended := h.since(into, time.Minute, func(m wsconn.Message) bool { return m.Type == "MatchEnded" })
	if ended < 0 {
		t.Fatalf("no outcome; heard %v", h.types(into))
	}
	var phases []clientv1.MatchPhase
	var kit *clientv1.StatsChanged
	for _, m := range h.got[into:ended] {
		switch d := m.Data.(type) {
		case *clientv1.MatchState:
			phases = append(phases, d.GetPhase())
		case *clientv1.StatsChanged:
			if kit == nil && d.GetTimers() != nil {
				kit = d
			}
		}
	}
	result := h.got[ended].Data.(*clientv1.MatchEnded)
	t.Logf("phases %v, outcome %v", phases, result.GetResult())
	if kit == nil || len(kit.GetTimers().GetTimers()) != 0 || kit.GetSheet() == nil {
		t.Fatalf("the sheet in the match %+v, want the kit's with no timers", kit)
	}
	if len(phases) < 3 || phases[0] != clientv1.MatchPhase_MATCH_PHASE_PREP || phases[len(phases)-1] != clientv1.MatchPhase_MATCH_PHASE_RESULT {
		t.Fatalf("phases %v, want prep through to the result", phases)
	}
	if r := result.GetRating(); r == nil || r.GetBefore() != 1000 || r.GetAfter() == r.GetBefore() || r.GetPlacementLeft() != 4 {
		t.Fatalf("rating change %+v", result.GetRating())
	}

	// The way back and the outcome race: the map's result phase against the store.
	out := h.since(into+1, time.Minute, func(m wsconn.Message) bool { return m.Type == "MapChange" })
	if out < 0 {
		t.Fatalf("no way back; heard %v", h.types(into))
	}
	back := h.got[out].Data.(*clientv1.MapChange)
	if back.GetMapId() != "town" || back.GetX() != home.GetX() || back.GetZ() != home.GetZ() {
		t.Fatalf("back at %+v, want town at (%v, %v)", back, home.GetX(), home.GetZ())
	}
	k = h.since(out, time.Minute, func(m wsconn.Message) bool {
		sc, ok := m.Data.(*clientv1.StatsChanged)
		return ok && sc.GetTimers() != nil
	})
	if k < 0 {
		t.Fatal("no timers back home")
	}
	if ts := h.got[k].Data.(*clientv1.StatsChanged).GetTimers().GetTimers(); len(ts) != 1 || ts[0].GetSkill() != "war_cry" || ts[0].GetLeftMs() < 59*60*1000 {
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

// heard keeps every message the player hears, so a wait finds one that came while the test waited for another.
type heard struct {
	l   *e2etest.Looter
	got []wsconn.Message
}

// since is the index of the first message from index from on that match takes, reading on for up to d; -1 for none.
func (h *heard) since(from int, d time.Duration, match func(wsconn.Message) bool) int {
	for k := from; k < len(h.got); k++ {
		if match(h.got[k]) {
			return k
		}
	}
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		m, ok := h.l.Read(time.Until(deadline))
		if !ok {
			return -1
		}
		h.l.Observe(m)
		h.got = append(h.got, m)
		if match(m) {
			return len(h.got) - 1
		}
	}
	return -1
}

// message is the k-th message heard, nil for -1.
func (h *heard) message(k int) any {
	if k < 0 {
		return nil
	}
	return h.got[k]
}

// types are the kinds of message heard from index from on.
func (h *heard) types(from int) []string {
	var out []string
	for _, m := range h.got[from:] {
		out = append(out, m.Type)
	}
	return out
}

// order says whether the message at k came before or after req's Response.
func (h *heard) order(from, k int, req string) string {
	for j := from; j < k; j++ {
		if h.got[j].Type == "Response" && h.got[j].ReqID == req {
			return "after the Response"
		}
	}
	return "before the Response"
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
