package e2e_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// I1/I2: a player queues for 1v1, meets a bot, accepts, fights its one round to the end, hears the outcome with their rating, and goes back to where they queued with the cooldown they had; their rating row has moved. Rounds are short here; every wait is for a message, a minute its bound, and finds one that came early.
func TestADuelAgainstABotRunsToItsEnd(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t,
		e2etest.DataEdit{File: "pvp.yaml", Old: "queue: {bots_after: 15s", New: "queue: {bots_after: 1s"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {prep: 10s, countdown: 3s, between: 5s}", New: "rounds: {prep: 500ms, countdown: 500ms, between: 500ms}"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "result: 10s", New: "result: 500ms"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {best_of: 3, most: 5, length: 60s}", New: "rounds: {best_of: 1, most: 1, length: 2s}"},
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
	// The phases come in the ring's frames and the outcome from the pvp module, two ways to the client, so either may come first.
	shown := h.since(into, time.Minute, func(m wsconn.Message) bool {
		st, ok := m.Data.(*clientv1.MatchState)
		return ok && st.GetPhase() == clientv1.MatchPhase_MATCH_PHASE_RESULT
	})
	if shown < 0 {
		t.Fatalf("no result phase; heard %v", h.types(into))
	}
	var phases []clientv1.MatchPhase
	var kit *clientv1.StatsChanged
	for _, m := range h.got[into : shown+1] {
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
	if back.GetMapId() != "willowmere" || back.GetX() != home.GetX() || back.GetZ() != home.GetZ() {
		t.Fatalf("back at %+v, want willowmere at (%v, %v)", back, home.GetX(), home.GetZ())
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

// I2: a party of two queues for 3v3 through its leader and meets four bots: both hear the ticket and the offer, accept, fight on one side with a bot beside them against three, hear the outcome and go back, and both rating rows in 3v3 have moved. Every wait is for a message, a minute its bound.
func TestAPartyOfTwoMeetsBotsIn3v3(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t,
		e2etest.DataEdit{File: "pvp.yaml", Old: "queue: {bots_after: 15s", New: "queue: {bots_after: 1s"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {prep: 10s, countdown: 3s, between: 5s}", New: "rounds: {prep: 500ms, countdown: 500ms, between: 500ms}"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "result: 10s", New: "result: 500ms"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {best_of: 3, most: 5, length: 90s}", New: "rounds: {best_of: 1, most: 1, length: 2s}"},
	))
	ctx := context.Background()
	var ls []*e2etest.Looter
	var ids []string
	for _, c := range []struct{ user, name, class string }{{"lead", "Lead", "warrior"}, {"mate", "Mate", "magician"}} {
		token, id := s.CreateCharacter(t, c.user, c.name, c.class)
		p, snap := s.Join(t, token, id)
		ls, ids = append(ls, e2etest.NewLooter(p, snap)), append(ids, id)
	}
	lead, mate := ls[0], ls[1]
	if r := lead.Command(t, "inv", &clientv1.PartyInvite{CharacterId: mate.ID}); !r.GetOk() {
		t.Fatalf("invite: %+v", r)
	}
	inv := e2etest.Take(t, mate, "PartyInvited", &mate.Invites, 5*time.Second, nil)
	if r := mate.Command(t, "acc", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
		t.Fatalf("accept the invite: %+v", r)
	}
	lead.PartyState(t, 2)
	mate.PartyState(t, 2)
	hs := []*heard{{l: lead}, {l: mate}}
	if r := mate.Command(t, "q-mate", &clientv1.PvpQueue{Mode: "team"}); r.GetOk() || r.GetCode() != code("NOT_PARTY_LEADER") {
		t.Fatalf("the member queueing the party: %+v, want NOT_PARTY_LEADER", r)
	}
	lead.Send(wsconn.Message{ReqID: "q", Seq: lead.Seq, Data: &clientv1.PvpQueue{Mode: "team"}})
	if k := hs[0].since(0, time.Minute, func(m wsconn.Message) bool { return m.Type == "Response" && m.ReqID == "q" }); k < 0 || !hs[0].got[k].Data.(*clientv1.Response).GetOk() {
		t.Fatalf("queue: %+v", hs[0].message(k))
	}
	var offers []*clientv1.PvpOffer
	for k, h := range hs {
		q := h.since(0, time.Minute, func(m wsconn.Message) bool { return m.Type == "PvpQueued" })
		if q < 0 || len(h.got[q].Data.(*clientv1.PvpQueued).GetMembers()) != 2 {
			t.Fatalf("%s heard %v, want queued with both", ids[k], h.types(0))
		}
		o := h.since(q, time.Minute, func(m wsconn.Message) bool { return m.Type == "PvpOffer" || m.Type == "PvpUnqueued" })
		if o < 0 || h.got[o].Type != "PvpOffer" || h.got[o].Data.(*clientv1.PvpOffer).GetBots() != 4 {
			t.Fatalf("%s after queueing: %+v, want an offer with four bots", ids[k], h.message(o))
		}
		offers = append(offers, h.got[o].Data.(*clientv1.PvpOffer))
	}
	if offers[0].GetOffer() != offers[1].GetOffer() {
		t.Fatalf("offers %+v, want one for both", offers)
	}
	into := make([]int, 2)
	for _, h := range hs {
		h.l.Send(wsconn.Message{ReqID: "yes", Seq: h.l.Seq, Data: &clientv1.PvpAnswer{Offer: offers[0].GetOffer(), Accept: true}})
	}
	for k, h := range hs {
		into[k] = h.since(0, time.Minute, func(m wsconn.Message) bool { return m.Type == "MapChange" })
		if into[k] < 0 || h.got[into[k]].Data.(*clientv1.MapChange).GetMapId() != "ring" {
			t.Fatalf("%s after accepting: %v, want a map change into the ring", ids[k], h.types(0))
		}
	}
	st := hs[0].since(into[0], time.Minute, func(m wsconn.Message) bool { return m.Type == "MatchState" })
	if st < 0 {
		t.Fatalf("no match state; heard %v", hs[0].types(into[0]))
	}
	state := hs[0].got[st].Data.(*clientv1.MatchState)
	sides := state.GetSides()
	if len(sides) != 2 || len(sides[0].GetFighters()) != 3 || len(sides[1].GetFighters()) != 3 {
		t.Fatalf("sides %+v, want three a side", sides)
	}
	var ours *clientv1.MatchSide
	for _, side := range sides {
		for _, f := range side.GetFighters() {
			if f.GetId() == ids[0] {
				ours = side
			}
		}
	}
	bots := 0
	for _, f := range ours.GetFighters() {
		if f.GetBot() {
			bots++
		}
	}
	if ours == nil || bots != 1 || !slices.ContainsFunc(ours.GetFighters(), func(f *clientv1.MatchFighter) bool { return f.GetId() == ids[1] }) {
		t.Fatalf("sides %+v, want Lead and Mate on one side with a bot", sides)
	}
	for k, h := range hs {
		ended := h.since(into[k], time.Minute, func(m wsconn.Message) bool { return m.Type == "MatchEnded" })
		if ended < 0 || h.got[ended].Data.(*clientv1.MatchEnded).GetRating() == nil {
			t.Fatalf("%s: no rated outcome; heard %v", ids[k], h.types(into[k]))
		}
		if out := h.since(into[k]+1, time.Minute, func(m wsconn.Message) bool { return m.Type == "MapChange" }); out < 0 || h.got[out].Data.(*clientv1.MapChange).GetMapId() != "willowmere" {
			t.Fatalf("%s: no way back to Willowmere; heard %v", ids[k], h.types(into[k]))
		}
		var matches int
		if err := s.DB.QueryRow(ctx, `SELECT matches FROM pvp_ratings WHERE character_id = $1 AND mode = 'team'`, ids[k]).Scan(&matches); err != nil || matches != 1 {
			t.Fatalf("%s's 3v3 rating row: %d matches, %v", ids[k], matches, err)
		}
	}
}

// pvp.md section 4: a duelist whose socket drops mid-round and who says Hello again within the grace is on their body in the match, which runs on; dropped again and back past the grace, they've left, the match is rated, and they land where they queued. Every wait is for a message or the rating row, a minute its bound.
func TestADroppedDuelistIsHeldThroughTheGraceButNotPastIt(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t,
		e2etest.DataEdit{File: "pvp.yaml", Old: "queue: {bots_after: 15s", New: "queue: {bots_after: 1s"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {prep: 10s, countdown: 3s, between: 5s}", New: "rounds: {prep: 500ms, countdown: 500ms, between: 500ms}"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "result: 10s", New: "result: 500ms"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "length: 60s}, reconnect: 20s}", New: "length: 60s}, reconnect: 6s}"},
	))
	token, id := s.CreateCharacter(t, "dropper", "Dropper", "warrior")
	p, snap := s.Join(t, token, id)
	h := &heard{l: e2etest.NewLooter(p, snap)}
	ask := func(req string, msg any) int {
		t.Helper()
		from := len(h.got)
		h.l.Send(wsconn.Message{ReqID: req, Seq: h.l.Seq, Data: msg})
		if k := h.since(from, time.Minute, func(m wsconn.Message) bool { return m.Type == "Response" && m.ReqID == req }); k < 0 || !h.got[k].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("%s: no ok Response", req)
		}
		return from
	}
	from := ask("queue", &clientv1.PvpQueue{Mode: "duel"})
	k := h.since(from, time.Minute, func(m wsconn.Message) bool { return m.Type == "PvpOffer" })
	if k < 0 {
		t.Fatalf("no offer; heard %v", h.types(from))
	}
	from = ask("accept", &clientv1.PvpAnswer{Offer: h.got[k].Data.(*clientv1.PvpOffer).GetOffer(), Accept: true})
	fighting := func(m wsconn.Message) bool {
		st, ok := m.Data.(*clientv1.MatchState)
		return ok && st.GetPhase() == clientv1.MatchPhase_MATCH_PHASE_FIGHT
	}
	if k = h.since(from, time.Minute, fighting); k < 0 {
		t.Fatalf("no round under way; heard %v", h.types(from))
	}

	_ = h.l.Conn.Close(websocket.StatusNormalClosure, "dropped")
	l, w := s.Hello(t, h.l.Token, id)
	if w.GetMapId() != "ring" {
		t.Fatalf("back within the grace on %q, want the ring", w.GetMapId())
	}
	back := &heard{l: l}
	listed := func(m wsconn.Message) bool {
		st, ok := m.Data.(*clientv1.MatchState)
		if !ok {
			return false
		}
		for _, side := range st.GetSides() {
			for _, f := range side.GetFighters() {
				if f.GetId() == id {
					return !f.GetAway()
				}
			}
		}
		return false
	}
	if k = back.since(0, time.Minute, listed); k < 0 {
		t.Fatalf("no MatchState listing the duelist back; heard %v", back.types(0))
	}
	if k = back.since(k, time.Minute, func(m wsconn.Message) bool { return m.Type == "Delta" && ownView(m, id) != nil }); k < 0 {
		t.Fatalf("no frame of the duelist's body after the return; heard %v", back.types(0))
	}
	t.Logf("back on the ring at tick %d, the body in the frame at tick %d", w.GetTick(), back.got[k].Tick)

	_ = l.Conn.Close(websocket.StatusNormalClosure, "dropped again")
	ctx := context.Background()
	var matches int
	for deadline := time.Now().Add(time.Minute); matches == 0; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no rated outcome within a minute of the second drop")
		}
		if err := s.DB.QueryRow(ctx, `SELECT matches FROM pvp_ratings WHERE character_id = $1 AND mode = 'duel'`, id).Scan(&matches); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
	}
	if _, w = s.Hello(t, l.Token, id); w.GetMapId() != "willowmere" {
		t.Fatalf("back past the grace on %q, want Willowmere, where they queued", w.GetMapId())
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
	for game := h.l.Game(d); ; {
		m, ok := game.Next()
		if !ok {
			return -1
		}
		h.l.Observe(m)
		h.got = append(h.got, m)
		if match(m) {
			return len(h.got) - 1
		}
	}
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
