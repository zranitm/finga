package push

import "finga/services/gateway/internal/model"

// worldDelivered routes a delivery to its session. A repeat Subscribed means the stream broke,
// so every player rejoins off this goroutine: their Snapshots arrive on this same stream. So does a
// detached one; a delivery for a connection this gateway does not have takes its presence off the map.
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
