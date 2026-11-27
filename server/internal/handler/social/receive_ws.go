package social

import (
	"context"

	clientv1 "finga/api/gen/client/v1"
	application "finga/internal/application/account"
	"finga/internal/application/social"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// Social is the social module as the client's party messages call it.
type Social interface {
	Invite(ctx context.Context, from, to kernel.CharacterID) (social.InviteID, error)
	Respond(ctx context.Context, who kernel.CharacterID, invite social.InviteID, accept bool) error
	LeaveParty(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
}

// Chat cleans a line through the social module and hands a map line to the world, which takes two modules, so the account's flow does it.
type Chat interface {
	Chat(ctx context.Context, s *application.Connection, channel model.ChatChannel, text string) error
}

// ws is the module's client messages: client to server.
type ws struct {
	social Social
	chat   Chat
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(s Social, chat Chat) transport.Routes {
	h := &ws{social: s, chat: chat}
	return func(r *transport.Receiver) {
		transport.Handle(r, "PartyInvite", h.partyInvite, r.Command(transport.LimitParty, r.Respond)...)
		transport.Handle(r, "PartyRespond", h.partyRespond, r.Command(transport.LimitParty, r.Respond)...)
		transport.Handle(r, "PartyLeave", h.partyLeave, r.Command(transport.LimitParty, r.Respond)...)
		transport.Handle(r, "PartyKick", h.partyKick, r.Command(transport.LimitParty, r.Respond)...)
		transport.Handle(r, "PartyPromote", h.partyPromote, r.Command(transport.LimitParty, r.Respond)...)
		// No limit here: the social module answers chat_rate_limited itself.
		transport.Handle(r, "ChatSend", h.chatSend, r.Queued, r.Traced, r.Joined, r.Within, r.RespondOnError)
	}
}

func (h *ws) partyInvite(c *transport.Context, in *clientv1.PartyInvite) (any, error) {
	to, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	_, err = h.social.Invite(c.Ctx, c.Session.Session().Character(), to)
	return nil, err
}

func (h *ws) partyRespond(c *transport.Context, in *clientv1.PartyRespond) (any, error) {
	invite, err := social.ParseInviteID(in.GetInviteId())
	if err != nil {
		return nil, err
	}
	return nil, h.social.Respond(c.Ctx, c.Session.Session().Character(), invite, in.GetAccept())
}

func (h *ws) partyLeave(c *transport.Context, _ *clientv1.PartyLeave) (any, error) {
	return nil, h.social.LeaveParty(c.Ctx, c.Session.Session().Character())
}

func (h *ws) partyKick(c *transport.Context, in *clientv1.PartyKick) (any, error) {
	target, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.social.Kick(c.Ctx, c.Session.Session().Character(), target)
}

func (h *ws) partyPromote(c *transport.Context, in *clientv1.PartyPromote) (any, error) {
	target, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.social.Promote(c.Ctx, c.Session.Session().Character(), target)
}

func (h *ws) chatSend(c *transport.Context, in *clientv1.ChatSend) (any, error) {
	return nil, h.chat.Chat(c.Ctx, c.Session.Session(), channelsFromClient[in.GetChannel()], in.GetText())
}

// channelsFromClient maps the client's channels to the gateway's; any other is no channel, which the social module refuses.
var channelsFromClient = map[clientv1.ChatChannel]model.ChatChannel{
	clientv1.ChatChannel_CHAT_CHANNEL_MAP:   model.ChatMap,
	clientv1.ChatChannel_CHAT_CHANNEL_PARTY: model.ChatParty,
}
