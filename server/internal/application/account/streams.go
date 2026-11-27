// Package push is the gateway's push flow: what the modules' streams and its timers start, routed to each session.
package account

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// Each Follow call resubscribes after a break until ctx ends.

// StreamsWorld is the world module's stream of deliveries for this gateway's players.
type StreamsWorld interface {
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent))
}

// StreamsSocial is the social module's stream of party and chat deliveries.
type StreamsSocial interface {
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.SocialEvent))
}

// StreamsPvp is the pvp module's stream of queue, offer and outcome pushes.
type StreamsPvp interface {
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.PvpDelivery))
	// Told marks the player told of matches, their outcomes handed to them.
	Told(ctx context.Context, character kernel.CharacterID, matches []kernel.MatchID) error
}

// StreamsAccount is the account module's stream of ended login sessions.
type StreamsAccount interface {
	FollowSessionEnds(ctx context.Context, gateway string, deliver func(model.SessionEnd))
}

// StreamsSessions checks that a login session still holds; the minute check asks it.
type StreamsSessions interface {
	Check(ctx context.Context, sessionID kernel.SessionID) error
}

// StreamsDeps are what the push flow is built from.
type StreamsDeps struct {
	Sessions StreamsSessions
	Registry *Connections // the sessions both flows share
	World    StreamsWorld
	Social   StreamsSocial
	Pvp      StreamsPvp
	Account  StreamsAccount
	Logger   *slog.Logger
	Panics   *panics.Recorder     // a round's call for one player, which runs on its own goroutine
	Traces   trace.TracerProvider // each background round's trace, never kept: it joins the round's log lines and calls
}

// StreamsOptions tune the push flow.
type StreamsOptions struct {
	SessionCheckInterval time.Duration // how often every session is checked with StreamsSessions
}

// Streams runs the push flow over the registry's sessions.
type Streams struct {
	deps       StreamsDeps
	opts       StreamsOptions
	tracer     trace.Tracer
	sessions   *Connections
	reregister chan struct{}          // a social subscription asks for every player to be registered again
	told       chan model.PvpDelivery // outcomes handed on live to players away from their match's end, for markTold
}

// NewStreams builds the push flow over deps.
func NewStreams(deps StreamsDeps, opts StreamsOptions) *Streams {
	return &Streams{deps: deps, opts: opts, tracer: deps.Traces.Tracer(streamsTracer), sessions: deps.Registry, reregister: make(chan struct{}, 1), told: make(chan model.PvpDelivery, toldQueue)}
}

const streamsTracer = "finga/internal/application/gateway/push"

// Run follows the modules' streams and periodically checks the sessions, until ctx is done or one of them panics.
func (g *Streams) Run(ctx context.Context) error {
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
	start(func(ctx context.Context) { g.deps.Pvp.FollowDeliveries(ctx, id, g.pvpDelivered) })
	start(g.markTold)
	start(g.registerAgain)
	start(func(ctx context.Context) { g.deps.Account.FollowSessionEnds(ctx, id, g.sessionEnded) })
	start(g.checkSessionsEvery)
	return group.Wait()
}
