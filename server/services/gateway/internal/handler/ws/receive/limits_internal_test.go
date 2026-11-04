package receive

import (
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/time/rate"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
)

// Input allows its burst of 30 at one instant, and 30 more a second later.
func TestALimitedCommandAllowsItsBurstThenRefills(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := slog.New(slog.DiscardHandler)
		h := New(send.New(send.Deps{Logger: log}, session.NewRegistry(session.Deps{Logger: log})), Options{Logger: log})
		// Past Hello, with a queue nobody drains: the queue holds what the limit let through.
		in := &Inbox{helloSeen: true, limiters: map[string]*rate.Limiter{}, queue: make(chan func(), 80)}
		inputs := func() {
			for range 40 {
				h.Dispatch(in, wsconn.Message{Type: "Input", Data: &clientv1.Input{Right: true}})
			}
		}
		inputs()
		if len(in.queue) != 30 {
			t.Fatalf("%d of 40 inputs at one instant passed, want the burst of 30", len(in.queue))
		}
		time.Sleep(time.Second)
		inputs()
		if len(in.queue) != 60 {
			t.Fatalf("%d passed after a second, want 30 more", len(in.queue))
		}
	})
}
