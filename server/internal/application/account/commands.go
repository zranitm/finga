package account

import (
	"context"
	"log/slog"
	"time"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// Ports fail with a failure.ServiceError.

// CommandsSessions checks a Hello's token and rotates it.
type CommandsSessions interface {
	Validate(ctx context.Context, token string) (model.Principal, error)
	Rotate(ctx context.Context, sessionID kernel.SessionID) (string, error)
}

// CommandsWorld is the world module's map chat: a cleaned map line goes to the player's map, fire and forget.
type CommandsWorld interface {
	Say(ctx context.Context, p model.Presence, name, text string) error
}

// CommandsSocial is the social module's chat.
type CommandsSocial interface {
	// Chat cleans and rate-limits a line; the caller hands map lines to the world.
	Chat(ctx context.Context, sender model.Player, channel model.ChatChannel, text string) (model.ChatLine, error)
}

// CommandsObserver counts the players online.
type CommandsObserver interface {
	OnlineChanged(delta int)
}

// CommandsDeps are what Hello and chat are built from.
type CommandsDeps struct {
	Sessions CommandsSessions
	Registry *Connections // the sessions both flows share
	World    CommandsWorld
	Social   CommandsSocial
	Logger   *slog.Logger
	Observer CommandsObserver
}

// CommandsOptions tune the Hello.
type CommandsOptions struct {
	JoinTimeout time.Duration // a Hello's whole join, retries included
}

const rotateTimeout = 5 * time.Second

// Commands runs a connection's Hello and its chat over the registry's connections.
type Commands struct {
	deps     CommandsDeps
	opts     CommandsOptions
	sessions *Connections
}

// NewCommands builds Hello and chat over deps.
func NewCommands(deps CommandsDeps, opts CommandsOptions) *Commands {
	return &Commands{deps: deps, opts: opts, sessions: deps.Registry}
}

// Draining reports whether the gateway drains; new connections are refused from then on.
func (g *Commands) Draining() bool { return g.sessions.Draining() }
