// Package send is the gateway's send flow, from a connection's Hello to its close; the handler chooses each reply.
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

// CommandsWorld is the world module's commands; each is fire and forget, but Equip, Unequip, UseItem, SpendPoints, ResetPoints, SpendSkillPoints and ChooseTrait, which answer once the new stats apply, UseSkill, Promote and the quest commands, which answer once applied, Teleport and SwitchChannel, which answer once their channel starts, and Channels and Bosses, which answer their lists.
type CommandsWorld interface {
	Input(ctx context.Context, p model.Presence, seq uint32, keys model.Keys, view uint32) error
	Attack(ctx context.Context, p model.Presence, aim float32, target model.Target) error
	EnterPortal(ctx context.Context, p model.Presence) error
	Teleport(ctx context.Context, p model.Presence, mapID, waypoint string, seq uint32) error
	SwitchChannel(ctx context.Context, p model.Presence, n int, seq uint32) error
	Channels(ctx context.Context, p model.Presence) (model.Channels, error)
	Bosses(ctx context.Context, p model.Presence) ([]model.AreaBoss, error)
	Say(ctx context.Context, p model.Presence, name, text string) error
	Equip(ctx context.Context, p model.Presence, in model.ItemEquip) error
	Unequip(ctx context.Context, p model.Presence, in model.ItemUnequip) error
	UseItem(ctx context.Context, p model.Presence, in model.ItemUse) error
	SpendPoints(ctx context.Context, p model.Presence, in model.PointsSpend) error
	ResetPoints(ctx context.Context, p model.Presence, expectedUnspent int) error
	SpendSkillPoints(ctx context.Context, p model.Presence, in model.SkillPointsSpend) error
	ChooseTrait(ctx context.Context, p model.Presence, in model.TraitChoice) error
	SetLook(ctx context.Context, p model.Presence, in model.LookSet) error
	TakeLoot(ctx context.Context, p model.Presence, picks []model.LootPick) ([]model.PickAnswer, error)
	UseSkill(ctx context.Context, p model.Presence, in model.SkillUse, seq uint32) error
	Talk(ctx context.Context, p model.Presence, npc string) error
	AcceptQuest(ctx context.Context, p model.Presence, in model.QuestAsk) error
	AbandonQuest(ctx context.Context, p model.Presence, quest string) error
	HandInQuest(ctx context.Context, p model.Presence, in model.QuestAsk) error
	Promote(ctx context.Context, p model.Presence, npc, class string) error
	Forfeit(ctx context.Context, p model.Presence) error
}

// CommandsPvp is the pvp module's queue.
type CommandsPvp interface {
	Queue(ctx context.Context, who model.Player, mode string) error
	LeaveQueue(ctx context.Context, who kernel.CharacterID) error
	Answer(ctx context.Context, who kernel.CharacterID, offer string, accept bool) error
}

// CommandsItems is the item module: the inventory and its commands.
type CommandsItems interface {
	Inventory(ctx context.Context, character kernel.CharacterID) (model.Inventory, error)
	Move(ctx context.Context, a model.Actor, in model.ItemMove) (model.InventoryChanged, error)
	Discard(ctx context.Context, a model.Actor, in model.ItemDiscard) (model.InventoryChanged, error)
	DiscardMany(ctx context.Context, a model.Actor, in model.ItemDiscardMany) (model.InventoryChanged, error)
	AddToWardrobe(ctx context.Context, a model.Actor, in model.WardrobeAdd) (model.WardrobeAdded, error)
}

// CommandsSocial is the social module's commands: parties and chat.
type CommandsSocial interface {
	// Chat cleans and rate-limits a line; the caller hands map lines to the world.
	Chat(ctx context.Context, sender model.Player, channel model.ChatChannel, text string) (model.ChatLine, error)
	Invite(ctx context.Context, from, to kernel.CharacterID) error
	Respond(ctx context.Context, who kernel.CharacterID, inviteID string, accept bool) error
	Leave(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
}

// CommandsObserver counts the players online.
type CommandsObserver interface {
	OnlineChanged(delta int)
}

// CommandsDeps are what the send flow is built from.
type CommandsDeps struct {
	Sessions CommandsSessions
	Registry *Connections // the sessions both flows share
	World    CommandsWorld
	Items    CommandsItems
	Social   CommandsSocial
	Pvp      CommandsPvp
	Logger   *slog.Logger
	Observer CommandsObserver
}

// CommandsOptions tune the send flow.
type CommandsOptions struct {
	JoinTimeout time.Duration // a Hello's whole join, retries included
}

const rotateTimeout = 5 * time.Second

// Commands runs the send flow's use cases over the registry's sessions.
type Commands struct {
	deps     CommandsDeps
	opts     CommandsOptions
	sessions *Connections
}

// NewCommands builds the send flow over deps.
func NewCommands(deps CommandsDeps, opts CommandsOptions) *Commands {
	return &Commands{deps: deps, opts: opts, sessions: deps.Registry}
}

// Draining reports whether the gateway drains; new connections are refused from then on.
func (g *Commands) Draining() bool { return g.sessions.Draining() }
