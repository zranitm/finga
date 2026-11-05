package push

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"finga/services/gateway/internal/model"
)

// Registering the players with the social service again: onlineAtOnce calls at a time, each with onlineTimeout.
const (
	onlineTimeout = 5 * time.Second
	onlineAtOnce  = 16
)

// socialDelivered forwards a delivery. Each subscription registers the players again, since the service
// takes a gateway's players offline once its stream stays gone; a new boot ID also means every party is gone.
func (g *Gateway) socialDelivered(ev model.SocialEvent) {
	if ev.Subscribed {
		restarted := g.socialBoot != "" && g.socialBoot != ev.BootID
		g.socialBoot = ev.BootID
		if restarted {
			for _, s := range g.sessions.Playing() {
				if s.Active() {
					s.DeliverSocial(model.Delivery{Push: model.PartyState{}})
				}
			}
		}
		select {
		case g.reregister <- struct{}{}:
		default: // one is already due, and will see these players
		}
		return
	}
	if s := g.sessions.ByCharacter(ev.Character); s != nil {
		s.DeliverSocial(ev.Delivery)
	}
}

// registerAgain registers every active player with the social service each time a subscription asks, off
// the stream's goroutine, so a slow service holds up no delivery.
func (g *Gateway) registerAgain(ctx context.Context) {
	for {
		select {
		case <-g.reregister:
		case <-ctx.Done():
			return
		}
		var round errgroup.Group
		round.SetLimit(onlineAtOnce)
		for _, s := range g.sessions.Playing() {
			if !s.Active() {
				continue // its Hello registers it
			}
			round.Go(func() error {
				ctx, cancel := context.WithTimeout(ctx, onlineTimeout)
				defer cancel()
				g.sessions.Online(ctx, s)
				return nil
			})
		}
		_ = round.Wait()
	}
}
