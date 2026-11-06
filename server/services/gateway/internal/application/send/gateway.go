// Package send is the gateway's send flow, from a connection's Hello to its close; the handler chooses each reply.
package send

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// Ports fail with a model.ServiceError.

// Sessions checks a Hello's token and rotates it.
type Sessions interface {
	Validate(ctx context.Context, token string) (auth.Principal, error)
	Rotate(ctx context.Context, sessionID uuid.UUID) (string, error)
}

// World is the world service's commands; each is fire and forget.
type World interface {
	Input(ctx context.Context, p model.Presence, seq uint32, keys model.Keys) error
	Attack(ctx context.Context, p model.Presence) error
	EnterPortal(ctx context.Context, p model.Presence) error
	PickUp(ctx context.Context, p model.Presence) error
	Say(ctx context.Context, p model.Presence, name, text string) error
}

// Items is the item service: the inventory and its commands.
type Items interface {
	Inventory(ctx context.Context, character kernel.CharacterID) ([]model.ItemView, error)
	Move(ctx context.Context, a model.Actor, in model.ItemMove) ([]model.InventorySlot, error)
	Discard(ctx context.Context, a model.Actor, in model.ItemDiscard) ([]model.InventorySlot, error)
}

// Social is the social service's commands: parties and chat.
type Social interface {
	// Chat cleans and rate-limits a line; the caller hands map lines to the world.
	Chat(ctx context.Context, sender model.Player, channel model.ChatChannel, text string) (model.ChatLine, error)
	Invite(ctx context.Context, from, to kernel.CharacterID) error
	Respond(ctx context.Context, who kernel.CharacterID, inviteID string, accept bool) error
	Leave(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
}

// Observer counts the players online.
type Observer interface {
	OnlineChanged(delta int)
}

// Deps are the services and settings the send flow is built from.
type Deps struct {
	Sessions Sessions
	Registry *session.Registry // the sessions both flows share
	World    World
	Items    Items
	Social   Social
	Logger   *slog.Logger
	Observer Observer
}

// Options tune the send flow.
type Options struct {
	JoinTimeout time.Duration // a Hello's whole join, retries included
}

const rotateTimeout = 5 * time.Second

// Gateway runs the send flow's use cases over the registry's sessions.
type Gateway struct {
	deps     Deps
	opts     Options
	sessions *session.Registry
}

// New builds the send flow over deps.
func New(deps Deps, opts Options) *Gateway {
	return &Gateway{deps: deps, opts: opts, sessions: deps.Registry}
}

// Draining reports whether the gateway drains; new connections are refused from then on.
func (g *Gateway) Draining() bool { return g.sessions.Draining() }
