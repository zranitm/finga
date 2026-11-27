package transport_test

import (
	"finga/internal/shared/failure"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Input allows its burst of 30 at one instant, and 30 more a second later, at the 20 Hz the world ticks at. Under a fake clock nothing refills between the sends.
func TestALimitedCommandAllowsItsBurstThenRefills(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fw := newFakeWorld()
		fw.tickRate = 20
		h := newHarnessOn(t, fw)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		inputs := func() {
			for seq := range uint32(40) {
				c.send(wsconn.Message{Seq: seq + 1, Data: &clientv1.Input{Right: true}})
			}
		}
		attacked := 0
		barrier := func() { // commands reach the world in order: once the Attack is in, every input before it was handled
			c.send(wsconn.Message{Data: &clientv1.Attack{}})
			attacked++
			waitFor(t, "the attack", h.world.attackList, attacked)
		}
		inputs()
		barrier()
		if got := h.world.inputCount(); got != 30 {
			t.Fatalf("%d of 40 inputs at one instant passed, want the burst of 30", got)
		}
		time.Sleep(time.Second)
		inputs()
		barrier()
		if got := h.world.inputCount(); got != 60 {
			t.Fatalf("%d passed after a second, want 30 more", got)
		}
	})
}

// A limit is a budget a group of messages shares, per connection: party commands 5, inventory and equipment 5, potions 5 of their own, skills 8, one a bar key, a portal 2, a teleport 2 of its own. A message over its budget with a request ID is answered rate_limited, at once, on the read loop.
func TestGroupedCommandsShareOneBudget(t *testing.T) {
	repeat := func(m wsconn.Message, n int) []wsconn.Message {
		out := make([]wsconn.Message, n)
		for i := range out {
			out[i] = m
		}
		return out
	}
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
		{"ten skills at one instant: one for each of the bar's eight keys", repeat(wsconn.Message{Type: "UseSkill", Data: &clientv1.UseSkill{}}, 10), 8},
		{"five portals at one instant", repeat(wsconn.Message{Type: "EnterPortal", Data: &clientv1.EnterPortal{}}, 5), 2},
		{"five teleports and five portals at one instant", append(
			repeat(wsconn.Message{Type: "Teleport", Data: &clientv1.Teleport{}}, 5),
			repeat(wsconn.Message{Type: "EnterPortal", Data: &clientv1.EnterPortal{}}, 5)...,
		), 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fw := newFakeWorld()
				fw.tickRate = 20
				h := newHarnessOn(t, fw)
				c := h.dial(t)
				c.hello(tokenAlice, aylaID)
				c.expect("Snapshot")
				for i, m := range tt.msgs {
					m.ReqID = fmt.Sprintf("r-%d", i)
					c.send(m)
				}
				c.send(wsconn.Message{ReqID: "ping", Data: &clientv1.Ping{}}) // the read loop answers a Ping after every refusal before it
				refused := 0
				for {
					m, _ := c.read()
					if m.Type == "Pong" {
						break
					}
					if r, ok := m.Data.(*clientv1.Response); ok && r.Code == int32(failure.RateLimited.Code) {
						refused++
					}
				}
				if got := len(tt.msgs) - refused; got != tt.want {
					t.Fatalf("%d of %d passed, want %d", got, len(tt.msgs), tt.want)
				}
			})
		})
	}
}
