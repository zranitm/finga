// Package push is the gateway's push flow: what the services' streams and its timers start, routed to each session.
package push

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"finga/internal/kernel"
	"finga/modules/gateway/internal/application/session"
	"finga/modules/gateway/internal/model"
	"finga/pkg/panics"
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
	Check(ctx context.Context, sessionID kernel.SessionID) error
}

// Deps are the services and settings the push flow is built from.
type Deps struct {
	Sessions Sessions
	Registry *session.Registry // the sessions both flows share
	World    World
	Social   Social
	Account  Account
	Logger   *slog.Logger
	Panics   *panics.Recorder     // a round's call for one player, which runs on its own goroutine
	Traces   trace.TracerProvider // each background round's trace, never kept: it joins the round's log lines and calls
}

// Options tune the push flow.
type Options struct {
	SessionCheckInterval time.Duration // how often every session is checked with Sessions
}

// Gateway runs the push flow over the registry's sessions.
type Gateway struct {
	deps       Deps
	opts       Options
	tracer     trace.Tracer
	sessions   *session.Registry
	reregister chan struct{} // a social subscription asks for every player to be registered again
}

// New builds the push flow over deps.
func New(deps Deps, opts Options) *Gateway {
	return &Gateway{deps: deps, opts: opts, tracer: deps.Traces.Tracer(tracerName), sessions: deps.Registry, reregister: make(chan struct{}, 1)}
}

const tracerName = "finga/modules/gateway/internal/application/push"

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
