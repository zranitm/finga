// Package push is the gateway's push flow: what the services' streams and the gateway's own timers
// start, each routed to the player's session it is for.
package push

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"finga/pkg/panics"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// Each Follow call resubscribes after a break until ctx ends.

// World is the world service's stream of deliveries for this gateway's players.
type World interface {
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent))
}

// Social is the social service's stream of party and chat deliveries.
type Social interface {
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.SocialEvent))
}

// Account is the account service's stream of ended login sessions.
type Account interface {
	FollowSessionEnds(ctx context.Context, gateway string, deliver func(model.SessionEnd))
}

// Sessions checks that a login session still holds; the minute check asks it.
type Sessions interface {
	Check(ctx context.Context, sessionID uuid.UUID) error
}

// Deps are the services and settings the push flow is built from.
type Deps struct {
	Sessions             Sessions
	World                World
	Social               Social
	Account              Account
	Logger               *slog.Logger
	SessionCheckInterval time.Duration // how often every session is checked with Sessions
}

// Gateway runs the push flow over the registry's sessions.
type Gateway struct {
	deps       Deps
	sessions   *session.Registry
	socialBoot string        // owned by the social stream; a new boot ID means the service restarted
	reregister chan struct{} // a social subscription asks for every player to be registered again
}

// New builds the push flow over sessions.
func New(deps Deps, sessions *session.Registry) *Gateway {
	return &Gateway{deps: deps, sessions: sessions, reregister: make(chan struct{}, 1)}
}

// Run follows the services' streams and periodically checks the sessions, until ctx is done or one of them panics.
func (g *Gateway) Run(ctx context.Context) error {
	group, gctx := errgroup.WithContext(ctx)
	start := func(f func(ctx context.Context)) {
		group.Go(func() (err error) {
			defer panics.Capture(&err)
			f(gctx)
			return nil
		})
	}
	id := g.sessions.Gateway()
	start(func(ctx context.Context) { g.deps.World.FollowDeliveries(ctx, id, g.worldDelivered) })
	start(func(ctx context.Context) { g.deps.Social.FollowDeliveries(ctx, id, g.socialDelivered) })
	start(g.registerAgain)
	start(func(ctx context.Context) { g.deps.Account.FollowSessionEnds(ctx, id, g.sessionEnded) })
	start(g.checkSessionsEvery)
	return group.Wait()
}
