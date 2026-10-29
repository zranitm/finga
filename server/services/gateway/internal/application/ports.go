package application

import (
	"context"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/services/gateway/internal/model"
)

// The services the gateway calls (page 10), as its use cases need them.
// The clients layer implements each over gRPC and hands every failure over
// as a model.ServiceError, so a use case knows what the client sees
// (model.PublicOf) and the kinds it acts on (model.ErrUnavailable,
// model.ErrNotSubscribed) without knowing gRPC. A follow call delivers a
// stream's events until ctx ends, subscribing again after a break.

// Accounts is the account service, for the REST API.
type Accounts interface {
	Register(ctx context.Context, username, password string) (model.Account, error)
	// Login returns a new session's token; ip is the caller's address, for
	// the per-IP throttle.
	Login(ctx context.Context, username, password, ip string) (string, error)
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

// World is the world service: joins, the players' commands, and the
// delivery stream that carries what each player sees.
type World interface {
	// Join joins a character through a connection of this gateway; the
	// world checks it is the account's. It fails with model.ErrUnavailable
	// while the world cannot be reached and model.ErrNotSubscribed until it
	// holds this gateway's stream.
	Join(ctx context.Context, gateway, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error)
	// Leave removes the player, if it joined through conn.
	Leave(ctx context.Context, p model.Presence, conn string) error
	Input(ctx context.Context, p model.Presence, seq uint32, in protocol.Input) error
	Attack(ctx context.Context, p model.Presence) error
	EnterPortal(ctx context.Context, p model.Presence) error
	PickUp(ctx context.Context, p model.Presence) error
	// Say hands a cleaned map line to the player's map instance.
	Say(ctx context.Context, p model.Presence, name, text string) error
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent))
}

// Items is the item service: the inventory and its commands.
type Items interface {
	Inventory(ctx context.Context, character kernel.CharacterID) ([]protocol.ItemView, error)
	// Move and Discard return the slots the command changed.
	Move(ctx context.Context, a model.Actor, in protocol.InventoryMove) ([]protocol.InventorySlot, error)
	Discard(ctx context.Context, a model.Actor, in protocol.InventoryDiscard) ([]protocol.InventorySlot, error)
}

// Social is the social service: parties and chat.
type Social interface {
	// PartyOf is the character's party; nil when it has none.
	PartyOf(ctx context.Context, character kernel.CharacterID) (*protocol.PartyView, error)
	// Online tells the service a player is connected through this gateway.
	Online(ctx context.Context, gateway string, p model.Player, mapID string) error
	Offline(ctx context.Context, character kernel.CharacterID) error
	// Chat cleans and limits a line; a party line is delivered by the
	// service itself, a map line is the caller's to hand to the world.
	Chat(ctx context.Context, sender model.Player, channel protocol.ChatChannel, text string) (model.ChatLine, error)
	Invite(ctx context.Context, from, to kernel.CharacterID) error
	Respond(ctx context.Context, who kernel.CharacterID, inviteID string, accept bool) error
	Leave(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
	FollowDeliveries(ctx context.Context, gateway string, deliver func(model.SocialEvent))
}

// Conn is one client connection as the use cases see it; the WebSocket
// handler implements it.
type Conn interface {
	ID() string
	// Send enqueues a message without blocking; a connection too slow to
	// keep up is closed by the transport.
	Send(m model.Message)
	// Close ends the connection; the first call wins.
	Close(code model.CloseCode, reason string)
}

// Observer counts traffic; internal/telemetry implements it.
type Observer interface {
	MessageIn(msgType string)
	MessageOut(msgType string)
	SlowConsumer()
	OnlineChanged(delta int)
}
