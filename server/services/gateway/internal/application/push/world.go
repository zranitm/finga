package push

import "finga/services/gateway/internal/model"

// worldDelivered routes a delivery to its session. A repeat Subscribed means the stream broke,
// so every player rejoins off this goroutine: their Snapshots arrive on this same stream. So does a
// detached one; a delivery for a connection this gateway does not have takes its presence off the map.
func (g *Gateway) worldDelivered(ev model.WorldEvent) {
	if ev.Subscribed {
		if broke, active := g.sessions.WorldSubscribed(); broke {
			for _, s := range active {
				g.sessions.Rejoin(s)
			}
		}
		return
	}
	s := g.sessions.ByConn(ev.Connection)
	switch {
	case s == nil:
		g.sessions.LeaveGhost(model.Presence{Character: ev.Character, Connection: ev.Connection})
	case ev.Detached:
		g.sessions.Rejoin(s)
	default:
		s.Deliver(ev.Delivery)
	}
}
