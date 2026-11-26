package e2etest

import (
	"fmt"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// fed is a looter joined on tick joined whose connection already holds queued, as when the test was busy elsewhere, and then brings msgs one at a time, each a moment after the last is read, as frames come a tick apart.
func fed(t *testing.T, joined uint64, queued []wsconn.Message, msgs ...wsconn.Message) *Looter {
	p := &Player{t: t, ID: "fed", msgs: make(chan wsconn.Message, len(queued)+1), done: make(chan struct{}), TickHz: 20, Joined: joined}
	for _, m := range queued {
		p.msgs <- m
	}
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
	l := fed(t, 5000, nil, append(frames(5001, 5040), reply("inv-1"))...)
	Take(t, l, "Response inv-1", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "inv-1" })
}

func TestAReplyReadDuringAWaitForTheGameIsKeptForALaterTake(t *testing.T) {
	t.Parallel()
	msgs := frames(101, 105)
	msgs = append(msgs, reply("inv-1"))
	msgs = append(msgs, frames(106, 110)...)
	l := fed(t, 100, nil, msgs...)
	if !l.Until(5*time.Second, func() bool { return l.Tick() >= 110 }) {
		t.Fatalf("the frames reached tick %d, want 110", l.Tick())
	}
	Take(t, l, "Response inv-1", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "inv-1" })
}

// From #350's CI: E6's retry waited behind 10 s of frames queued while it checked the database, and the 5 s were spent on them before its Response.
func TestFramesQueuedBeforeAWaitAreNotItsTime(t *testing.T) {
	t.Parallel()
	l := fed(t, 100, frames(101, 300), append(frames(301, 320), reply("e-2"))...)
	Take(t, l, "Response e-2", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "e-2" })
}

func TestARestartedWorldsClockCountsOneTickAtATimeFromItsStart(t *testing.T) {
	t.Parallel()
	msgs := frames(5001, 5010)
	msgs = append(msgs, frames(1, 60)...)
	l := fed(t, 5000, nil, append(msgs, reply("e-2"))...)
	Take(t, l, "Response e-2", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "e-2" })
}

func TestANewMapsClockCountsOneTickAtATimeFromItsStartWhicheverWayItLies(t *testing.T) {
	t.Parallel()
	for _, start := range []uint64{200, 9000} {
		msgs := frames(5001, 5010)
		msgs = append(msgs, wsconn.Message{Type: "MapChange", Data: &clientv1.MapChange{MapId: "willowmere"}}, wsconn.Message{Type: "Snapshot", Tick: start, Data: &clientv1.Snapshot{}})
		msgs = append(msgs, frames(start+1, start+50)...)
		l := fed(t, 5000, nil, append(msgs, reply("hop"))...)
		Take(t, l, fmt.Sprintf("Response hop onto a map at tick %d", start), &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "hop" })
	}
}
