package account

import (
	"finga/internal/application/world"
	model "finga/internal/model/account"
)

// delivered turns one world delivery into its events: a Frame into one per recipient, all sharing the frame.
func delivered(d world.Delivery, deliver func(model.WorldEvent)) {
	switch ev := d.Event.(type) {
	case world.Frame:
		frame := &ev
		for _, r := range ev.Recipients {
			deliver(model.WorldDelivery{Connection: r.Connection, Character: r.Character, Delivery: model.Delivery{Tick: d.Tick, Event: world.Framed{Frame: frame, Recipient: r}}})
		}
	case world.Detached:
		deliver(model.Detached{Connection: d.Conn, Character: d.Character})
	default:
		out := model.Delivery{Tick: d.Tick, Event: d.Event}
		switch ev := d.Event.(type) {
		case world.Snapshot:
			out.Starts = true
		case world.Teleport:
			out.MapID = ev.MapID
		}
		deliver(model.WorldDelivery{Connection: d.Conn, Character: d.Character, Delivery: out})
	}
}
