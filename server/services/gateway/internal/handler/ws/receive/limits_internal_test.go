package receive

import (
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"golang.org/x/time/rate"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
)

// Input allows its burst of 30 at one instant, and 30 more a second later, at the 20 Hz assumed before a join reports the rate.
func TestALimitedCommandAllowsItsBurstThenRefills(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := slog.New(slog.DiscardHandler)
		reg := session.NewRegistry(session.Deps{Logger: log, Traces: noop.NewTracerProvider()}, session.Options{})
		h := New(send.New(send.Deps{Logger: log, Registry: reg}, send.Options{}), Options{Logger: log, Traces: noop.NewTracerProvider()})
		// Past Hello, with a queue nobody drains: the queue holds what the limit let through.
		in := &Inbox{helloSeen: true, session: reg.Open("c-1", nil), limiters: map[string]*rate.Limiter{}, queue: make(chan func(), 80)}
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

// A limit is a budget a group of messages shares, per connection: party commands 5, inventory and equipment 5, potions 5 of their own, a portal 2.
func TestGroupedCommandsShareOneBudget(t *testing.T) {
	tests := []struct {
		name string
		msgs []wsconn.Message
		want int
	}{
		{"four party invites and four leaves at one instant", append(
			repeat(wsconn.Message{Type: "PartyInvite", Data: &clientv1.PartyInvite{}}, 4),
			repeat(wsconn.Message{Type: "PartyLeave", Data: &clientv1.PartyLeave{}}, 4)...,
		), 5},
		{"five moves and five discards at one instant", append(
			repeat(wsconn.Message{Type: "InventoryMove", Data: &clientv1.InventoryMove{}}, 5),
			repeat(wsconn.Message{Type: "InventoryDiscard", Data: &clientv1.InventoryDiscard{}}, 5)...,
		), 5},
		{"three equips, three unequips and a move at one instant", append(append(
			repeat(wsconn.Message{Type: "Equip", Data: &clientv1.Equip{}}, 3),
			repeat(wsconn.Message{Type: "Unequip", Data: &clientv1.Unequip{}}, 3)...),
			wsconn.Message{Type: "InventoryMove", Data: &clientv1.InventoryMove{}},
		), 5},
		{"eight potions and a move at one instant", append(
			repeat(wsconn.Message{Type: "UseItem", Data: &clientv1.UseItem{}}, 8),
			wsconn.Message{Type: "InventoryMove", Data: &clientv1.InventoryMove{}},
		), 6},
		{"five portals at one instant", repeat(wsconn.Message{Type: "EnterPortal", Data: &clientv1.EnterPortal{}}, 5), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				log := slog.New(slog.DiscardHandler)
				reg := session.NewRegistry(session.Deps{Logger: log, Traces: noop.NewTracerProvider()}, session.Options{})
				h := New(send.New(send.Deps{Logger: log, Registry: reg}, send.Options{}), Options{Logger: log, Traces: noop.NewTracerProvider()})
				in := &Inbox{helloSeen: true, session: reg.Open("c-1", nil), limiters: map[string]*rate.Limiter{}, queue: make(chan func(), 80)}
				for _, m := range tt.msgs {
					h.Dispatch(in, m)
				}
				if len(in.queue) != tt.want {
					t.Fatalf("%d of %d passed, want %d", len(in.queue), len(tt.msgs), tt.want)
				}
			})
		})
	}
}

func repeat(m wsconn.Message, n int) []wsconn.Message {
	out := make([]wsconn.Message, n)
	for i := range out {
		out[i] = m
	}
	return out
}
