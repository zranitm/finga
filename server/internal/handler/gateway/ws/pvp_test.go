package ws_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/pvp"
	model "finga/internal/model/gateway"
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
			{&clientv1.PvpQueue{Mode: "duel"}, "Queue " + aylaID.String() + " duel"},
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
		fw.mu.Lock()
		if len(fw.forfeits) != 1 || fw.forfeits[0].Character != aylaID {
			t.Fatalf("forfeits %+v, want Ayla's", fw.forfeits)
		}
		fw.mu.Unlock()
		time.Sleep(2 * time.Second) // the pvp budget refills
		fp.mu.Lock()
		fp.err = refusal(kernel.ErrPvpAlreadyQueued)
		fp.mu.Unlock()
		ayla.send(wsconn.Message{ReqID: "q-x", Data: &clientv1.PvpQueue{Mode: "duel"}})
		if r := ayla.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.PvpAlreadyQueued.Code) {
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
