package push

import (
	"context"
	"time"

	"finga/services/gateway/internal/model"
)

const onlineTimeout = 5 * time.Second

// socialDelivered forwards a delivery; a new boot ID means the service restarted and every party is gone.
func (g *Gateway) socialDelivered(ev model.SocialEvent) {
	if ev.Subscribed {
		restarted := g.socialBoot != "" && g.socialBoot != ev.BootID
		g.socialBoot = ev.BootID
		if !restarted {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), onlineTimeout)
		defer cancel()
		for _, s := range g.sessions.Playing() {
			s.DeliverNow(model.Delivery{Push: model.PartyState{}})
			g.sessions.Online(ctx, s)
		}
		return
	}
	if s := g.sessions.ByCharacter(ev.Character); s != nil {
		s.DeliverNow(ev.Delivery)
	}
}
