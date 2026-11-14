package push

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
)

// Registering the players with the social service again: onlineAtOnce calls at a time, each with onlineTimeout.
const (
	onlineTimeout = 5 * time.Second
	onlineAtOnce  = 16
)

// socialDelivered forwards a delivery to its player, or a broadcast to every player in the world; each subscription registers the players again, as the module may drop them.
func (g *Gateway) socialDelivered(ev model.SocialEvent) {
	switch ev := ev.(type) {
	case model.SocialSubscribed:
		g.socialSubscribed()
	case model.SocialDelivery:
		if s := g.sessions.ByCharacter(ev.Character); s != nil {
			s.DeliverSocial(ev.Delivery)
		}
	case model.SocialBroadcast:
		for _, s := range g.sessions.Playing() {
			if s.Active() {
				s.DeliverSocial(ev.Delivery)
			}
		}
	}
}

// socialSubscribed registers every active player again, as the module may have dropped them while the stream was broken.
func (g *Gateway) socialSubscribed() {
	select {
	case g.reregister <- struct{}{}:
	default: // one is already due, and will see these players
	}
}

// registerAgain registers every active player with the social module off the stream's goroutine, delaying no delivery.
func (g *Gateway) registerAgain(ctx context.Context) {
	for {
		select {
		case <-g.reregister:
		case <-ctx.Done():
			return
		}
		g.registerRound(ctx)
	}
}

// registerRound registers every active player once, all in one trace.
func (g *Gateway) registerRound(ctx context.Context) {
	ctx, span := g.tracer.Start(ctx, "gateway.register_again", trace.WithNewRoot())
	defer span.End()
	var round errgroup.Group
	round.SetLimit(onlineAtOnce)
	for _, s := range g.sessions.Playing() {
		if !s.Active() {
			continue // its Hello registers it
		}
		round.Go(func() error {
			g.deps.Panics.Do(ctx, panics.FlowJob, func() {
				ctx, cancel := context.WithTimeout(ctx, onlineTimeout)
				defer cancel()
				g.sessions.Online(ctx, s)
			}, "job", "register_again")
			return nil
		})
	}
	_ = round.Wait()
}
