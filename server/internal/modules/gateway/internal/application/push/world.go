package push

import "finga/modules/gateway/internal/model"

// worldDelivered routes a delivery to its session; rejoins run off this goroutine, as their Snapshots arrive on it.
func (g *Gateway) worldDelivered(ev model.WorldEvent) {
	switch ev := ev.(type) {
	case model.WorldSubscribed:
		if broke, active := g.sessions.WorldSubscribed(); broke {
			for _, s := range active {
				g.sessions.Rejoin(s)
			}
		}
	case model.Detached:
		if s := g.sessions.ByConn(ev.Connection); s != nil {
			g.sessions.Rejoin(s)
		} else {
			g.sessions.LeaveGhost(model.Presence{Character: ev.Character, Connection: ev.Connection})
		}
	case model.WorldDelivery:
		if s := g.sessions.ByConn(ev.Connection); s != nil {
			s.Deliver(ev.Delivery)
		} else {
			g.sessions.LeaveGhost(model.Presence{Character: ev.Character, Connection: ev.Connection})
		}
	}
}
