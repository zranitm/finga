package application

import (
	"context"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

// Ports fail with a model.ServiceError; Follow* calls resubscribe after a break until ctx ends.

// Accounts is the account service, for the REST API.
type Accounts interface {
	Register(ctx context.Context, username, password string) (model.Account, error)
	Login(ctx context.Context, username, password string) (string, error)
	Settings(ctx context.Context, account kernel.AccountID) (model.Settings, error)
	UpdateSettings(ctx context.Context, account kernel.AccountID, s model.Settings) error
}

// Characters is the character service, for the REST API.
type Characters interface {
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	Create(ctx context.Context, account kernel.AccountID, name, class string) (model.Character, error)
}

// SessionEnds is the account service's stream of ended login sessions.
type SessionEnds interface {
	FollowSessionEnds(ctx context.Context, gateway string, deliver func(model.SessionEnd))
}

// World is the world service: joins, commands and the delivery stream.
type World interface {
	// Join fails with model.ErrUnavailable or model.ErrNotSubscribed while the world cannot take it yet.
	Join(ctx context.Context, gateway, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error)
	// Leave removes the player, if it joined through conn.
	Leave(ctx context.Context, p model.Presence, conn string) error
	Input(ctx context.Context, p model.Presence, seq uint32, keys model.Keys) error
	Attack(ctx context.Context, p model.Presence) error
	EnterPortal(ctx context.Context, p model.Presence) error
	PickUp(ctx context.Context, p model.Presence) error
	Say(ctx context.Context, p model.Presence, name, text string) error
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent))
}

// Items is the item service: the inventory and its commands.
type Items interface {
	Inventory(ctx context.Context, character kernel.CharacterID) ([]*clientv1.ItemView, error)
	Move(ctx context.Context, a model.Actor, in model.ItemMove) ([]*clientv1.InventorySlot, error)
	Discard(ctx context.Context, a model.Actor, in model.ItemDiscard) ([]*clientv1.InventorySlot, error)
}

// Social is the social service: parties and chat.
type Social interface {
	// PartyOf is the character's party; nil when it has none.
	PartyOf(ctx context.Context, character kernel.CharacterID) (*clientv1.PartyView, error)
	Online(ctx context.Context, gateway string, p model.Player, mapID string) error
	Offline(ctx context.Context, character kernel.CharacterID) error
	// Chat cleans and rate-limits a line; the caller hands map lines to the world.
	Chat(ctx context.Context, sender model.Player, channel clientv1.ChatChannel, text string) (model.ChatLine, error)
	Invite(ctx context.Context, from, to kernel.CharacterID) error
	Respond(ctx context.Context, who kernel.CharacterID, inviteID string, accept bool) error
	Leave(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.SocialEvent))
}

// Outbox is how the use cases reach one player; the handler picks the wire format. No method blocks.
type Outbox interface {
	Welcome(w model.Welcome)
	// MapChange tells the player a rejoin put them on another map.
	MapChange(mapID string, x, y float64)
	// PartyGone tells the player their party is gone because the social service restarted.
	PartyGone()
	Deliver(d model.Delivery)
	// End ends the player's session; the first End wins.
	End(reason model.EndReason)
}

// Observer counts the players online.
type Observer interface {
	OnlineChanged(delta int)
}
