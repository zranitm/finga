package push

import "finga/services/gateway/internal/model"

// worldDelivered routes a delivery to its session. A repeat Subscribed means the stream broke,
// so every player rejoins off this goroutine: their Snapshots arrive on this same stream.
func (g *Gateway) worldDelivered(ev model.WorldEvent) {
	if ev.Subscribed {
		if broke, active := g.sessions.WorldSubscribed(); broke {
			for _, s := range active {
				g.sessions.Rejoin(s)
			}
		}
		return
	}
	if s := g.sessions.ByConn(ev.Connection); s != nil {
		s.Deliver(ev.Delivery)
	}
}
