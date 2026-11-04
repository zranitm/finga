// Package send is the gateway's send flow: what a player's connection asks for, from its Hello to its
// close. Each use case answers with its result or error, and the handler decides the reply.
package send

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
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
	Inventory(ctx context.Context, character kernel.CharacterID) ([]*clientv1.ItemView, error)
	Move(ctx context.Context, a model.Actor, in model.ItemMove) ([]*clientv1.InventorySlot, error)
	Discard(ctx context.Context, a model.Actor, in model.ItemDiscard) ([]*clientv1.InventorySlot, error)
}

// Social is the social service's commands: parties and chat.
type Social interface {
	// PartyOf is the character's party; nil when it has none.
	PartyOf(ctx context.Context, character kernel.CharacterID) (*clientv1.PartyView, error)
	Offline(ctx context.Context, character kernel.CharacterID) error
	// Chat cleans and rate-limits a line; the caller hands map lines to the world.
	Chat(ctx context.Context, sender model.Player, channel clientv1.ChatChannel, text string) (model.ChatLine, error)
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
	Sessions    Sessions
	World       World
	Items       Items  // nil: no inventory
	Social      Social // nil: no parties or chat
	Logger      *slog.Logger
	Observer    Observer
	JoinTimeout time.Duration // a Hello's whole join, retries included
}

const offlineTimeout = 5 * time.Second

// Gateway runs the send flow's use cases over the registry's sessions.
type Gateway struct {
	deps     Deps
	sessions *session.Registry
}

// New builds the send flow over sessions, defaulting unset dependencies.
func New(deps Deps, sessions *session.Registry) *Gateway {
	if deps.Observer == nil {
		deps.Observer = noopObserver{}
	}
	return &Gateway{deps: deps, sessions: sessions}
}

// Draining reports whether the gateway drains; new connections are refused from then on.
func (g *Gateway) Draining() bool { return g.sessions.Draining() }

type noopObserver struct{}

// OnlineChanged does nothing.
func (noopObserver) OnlineChanged(int) {}
