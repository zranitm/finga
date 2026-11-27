package transport_test

import (
	"finga/internal/shared/failure"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/pvp"
	"finga/internal/shared/kernel"
	"finga/pkg/wsconn"
)

// The four PvP commands reach their modules with the connection's character and answer their requests, a refusal with its code; the module's pushes reach the player they name; a logout leaves the queue.
func TestPvpCommandsAndPushes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fp := newFakePvp()
		fw := newFakeWorld()
		h := newHarnessOn(t, fw, func(d *deps) { d.Pvp = fp })
		select {
		case <-fp.pushes.subscribed:
		case <-time.After(5 * time.Second):
			t.Fatal("the gateway never subscribed to the pvp module")
		}
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Welcome")
		ayla.expect("Snapshot")
		for k, tt := range []struct {
			msg  any
			want string
		}{
			{&clientv1.PvpQueue{Mode: "duel"}, "Queue " + aylaID.String() + " Ayla duel"},
			{&clientv1.PvpAnswer{Offer: "o-1", Accept: true}, "Answer " + aylaID.String() + " o-1 true"},
			{&clientv1.PvpLeaveQueue{}, "LeaveQueue " + aylaID.String()},
		} {
			req := "q-" + string(rune('a'+k))
			ayla.send(wsconn.Message{ReqID: req, Data: tt.msg})
			if r := ayla.expect("Response"); r.ReqID != req || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("%T: response %+v", tt.msg, r)
			}
			if got := fp.recorded(); got[len(got)-1] != tt.want {
				t.Fatalf("%T: calls %q, want %q last", tt.msg, got, tt.want)
			}
		}
		ayla.send(wsconn.Message{ReqID: "q-f", Data: &clientv1.PvpForfeit{}})
		if r := ayla.expect("Response").Data.(*clientv1.Response); !r.GetOk() {
			t.Fatalf("forfeit: %+v", r)
		}
		conn := connectionOf(fw, aylaID)
		fw.mu.Lock()
		if len(fw.forfeits) != 1 || fw.forfeits[0].Character != aylaID || fw.forfeits[0].Conn != conn {
			t.Fatalf("forfeits %+v, want Ayla's", fw.forfeits)
		}
		fw.mu.Unlock()
		time.Sleep(2 * time.Second) // the pvp budget refills
		fp.mu.Lock()
		fp.err = refusal(kernel.ErrPvpAlreadyQueued)
		fp.mu.Unlock()
		ayla.send(wsconn.Message{ReqID: "q-x", Data: &clientv1.PvpQueue{Mode: "duel"}})
		if r := ayla.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(failure.PvpAlreadyQueued.Code) {
			t.Fatalf("queued twice: %+v", r)
		}
		for _, id := range []kernel.CharacterID{aylaID, robinID} { // Robin isn't on this gateway: dropped
			fp.pushes.send(pvp.Push{To: id, Delivery: pvp.Offered{Offer: "o-2", Mode: "duel", Bots: 1, Until: 10 * time.Second}})
		}
		if o := ayla.expect("PvpOffer").Data.(*clientv1.PvpOffer); o.GetOffer() != "o-2" || o.GetBots() != 1 || o.GetUntilMs() != 10000 {
			t.Fatalf("offer %+v", o)
		}
		_ = ayla.c.Close(websocket.StatusNormalClosure, "bye")
		synctest.Wait()
		if got := fp.recorded(); got[len(got)-1] != "Offline "+aylaID.String() {
			t.Fatalf("calls %q, want Ayla's Offline last", got)
		}
	})
}

// The matches that ended while the player was away reach their Welcome, which marks every one it lists or counts told once it's written; an outcome they were away for, handed to them live, is marked told, and neither one for a player on another gateway nor one they saw end is.
func TestMatchesMissedWhileAwayReachTheWelcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fp := newFakePvp()
		ended := time.UnixMilli(1_764_000_000_000)
		missed := []kernel.MatchID{kernel.NewMatchID(), kernel.NewMatchID(), kernel.NewMatchID(), kernel.NewMatchID(), kernel.NewMatchID()}
		fp.missed = map[kernel.CharacterID]pvp.Missed{aylaID: {Matches: []pvp.MissedMatch{{Match: missed[0], Mode: "duel", Outcome: pvp.Loss, Left: true, Ended: ended}, {Match: missed[1], Mode: "team", Outcome: pvp.Win, Ended: ended.Add(-time.Hour)}}, Older: missed[2:]}}
		h := newHarnessOn(t, newFakeWorld(), func(d *deps) { d.Pvp = fp })
		select {
		case <-fp.pushes.subscribed:
		case <-time.After(5 * time.Second):
			t.Fatal("the gateway never subscribed to the pvp module")
		}
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		w := ayla.expect("Welcome").Data.(*clientv1.Welcome)
		want := []*clientv1.MissedMatch{
			{Mode: "duel", Result: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS, Left: true, EndedAt: 1_764_000_000_000},
			{Mode: "team", Result: clientv1.MatchOutcome_MATCH_OUTCOME_WIN, EndedAt: 1_764_000_000_000 - 3_600_000},
		}
		if got := w.GetMissedMatches(); len(got) != len(want) || !proto.Equal(got[0], want[0]) || !proto.Equal(got[1], want[1]) || w.GetMissedMore() != 3 {
			t.Fatalf("Welcome's missed %v and %d more; want %v and 3 more", got, w.GetMissedMore(), want)
		}
		ayla.expect("Snapshot")
		synctest.Wait()
		welcomed := "Told " + aylaID.String()
		for _, m := range missed {
			welcomed += " " + m.String()
		}
		away, seen, elsewhere := kernel.NewMatchID(), kernel.NewMatchID(), kernel.NewMatchID()
		fp.pushes.send(pvp.Push{To: robinID, Delivery: pvp.Ended{Mode: "duel", Outcome: pvp.Loss, Left: true, Match: elsewhere, Away: true}})
		fp.pushes.send(pvp.Push{To: aylaID, Delivery: pvp.Ended{Mode: "duel", Outcome: pvp.Win, Match: seen}})
		fp.pushes.send(pvp.Push{To: aylaID, Delivery: pvp.Ended{Mode: "team", Outcome: pvp.Loss, Left: true, Match: away, Away: true}})
		ayla.expect("MatchEnded")
		ayla.expect("MatchEnded")
		synctest.Wait()
		var told []string
		for _, c := range fp.recorded() {
			if strings.HasPrefix(c, "Told ") {
				told = append(told, c)
			}
		}
		if want := []string{welcomed, "Told " + aylaID.String() + " " + away.String()}; !slices.Equal(told, want) {
			t.Fatalf("marked told %q; want %q", told, want)
		}
	})
}
