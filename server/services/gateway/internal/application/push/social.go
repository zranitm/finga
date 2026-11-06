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

// socialDelivered forwards a delivery; each subscription registers the players again, as the service may drop them.
func (g *Gateway) socialDelivered(ev model.SocialEvent) {
	switch ev := ev.(type) {
	case model.SocialSubscribed:
		g.socialSubscribed(ev.BootID)
	case model.SocialDelivery:
		if s := g.sessions.ByCharacter(ev.Character); s != nil {
			s.DeliverSocial(ev.Delivery)
		}
	}
}

func (g *Gateway) socialSubscribed(bootID string) {
	restarted := g.socialBoot != "" && g.socialBoot != bootID
	g.socialBoot = bootID
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
}

// registerAgain registers every active player with the social service off the stream's goroutine, delaying no delivery.
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
