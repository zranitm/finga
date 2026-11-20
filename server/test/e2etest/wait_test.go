package e2etest

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// fed is a looter joined on tick joined whose connection brings msgs one at a time, each a moment after the last is read, as frames come a tick apart.
func fed(t *testing.T, joined uint64, msgs ...wsconn.Message) *Looter {
	p := &Player{t: t, ID: "fed", msgs: make(chan wsconn.Message, 1), done: make(chan struct{}), TickHz: 20, Joined: joined}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for _, m := range msgs {
			for len(p.msgs) > 0 {
				time.Sleep(100 * time.Microsecond)
			}
			time.Sleep(time.Millisecond)
			select {
			case p.msgs <- m:
			case <-stop:
				return
			}
		}
	}()
	return NewLooter(p, &clientv1.Snapshot{})
}

// frames is a Delta for each tick from from to to.
func frames(from, to uint64) []wsconn.Message {
	var out []wsconn.Message
	for tick := from; tick <= to; tick++ {
		out = append(out, wsconn.Message{Type: "Delta", Tick: tick, Data: &clientv1.Delta{}})
	}
	return out
}

func reply(req string) wsconn.Message {
	return wsconn.Message{Type: "Response", ReqID: req, Data: &clientv1.Response{Ok: true}}
}

// From #347's CI: on a map 5000 ticks old, a reply 2 s after the join came after the whole 5 s had been counted on the first frame.
func TestAWaitStraightAfterAJoinCountsFromTheJoinsTick(t *testing.T) {
	t.Parallel()
	l := fed(t, 5000, append(frames(5001, 5040), reply("inv-1"))...)
	Take(t, l, "Response inv-1", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "inv-1" })
}

func TestAReplyReadDuringAWaitForTheGameIsKeptForALaterTake(t *testing.T) {
	t.Parallel()
	msgs := frames(101, 105)
	msgs = append(msgs, reply("inv-1"))
	msgs = append(msgs, frames(106, 110)...)
	l := fed(t, 100, msgs...)
	if !l.Until(5*time.Second, func() bool { return l.Tick() >= 110 }) {
		t.Fatalf("the frames reached tick %d, want 110", l.Tick())
	}
	Take(t, l, "Response inv-1", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "inv-1" })
}
