package push

import model "finga/internal/model/gateway"

// pvpDelivered hands a pvp push to its player, if they're on this gateway.
func (g *Gateway) pvpDelivered(ev model.PvpDelivery) {
	if s := g.sessions.ByCharacter(ev.Character); s != nil {
		s.DeliverSocial(ev.Delivery)
	}
}
