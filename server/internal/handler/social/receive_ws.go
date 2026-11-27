package social

import (
	clientv1 "finga/api/gen/client/v1"
	application "finga/internal/application/account"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// ws is the module's client messages: client to server.
type ws struct {
	gw *application.Commands
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(gw *application.Commands) transport.Routes {
	h := &ws{gw: gw}
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
	return nil, h.gw.InviteToParty(c.Ctx, c.Session.Session(), to)
}

func (h *ws) partyRespond(c *transport.Context, in *clientv1.PartyRespond) (any, error) {
	return nil, h.gw.AnswerInvite(c.Ctx, c.Session.Session(), in.GetInviteId(), in.GetAccept())
}

func (h *ws) partyLeave(c *transport.Context, _ *clientv1.PartyLeave) (any, error) {
	return nil, h.gw.LeaveParty(c.Ctx, c.Session.Session())
}

func (h *ws) partyKick(c *transport.Context, in *clientv1.PartyKick) (any, error) {
	target, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.KickFromParty(c.Ctx, c.Session.Session(), target)
}

func (h *ws) partyPromote(c *transport.Context, in *clientv1.PartyPromote) (any, error) {
	target, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return nil, h.gw.PromoteInParty(c.Ctx, c.Session.Session(), target)
}

func (h *ws) chatSend(c *transport.Context, in *clientv1.ChatSend) (any, error) {
	return nil, h.gw.Chat(c.Ctx, c.Session.Session(), channelsFromClient[in.GetChannel()], in.GetText())
}

// channelsFromClient maps the client's channels to the gateway's; any other is no channel, which the social module refuses.
var channelsFromClient = map[clientv1.ChatChannel]model.ChatChannel{
	clientv1.ChatChannel_CHAT_CHANNEL_MAP:   model.ChatMap,
	clientv1.ChatChannel_CHAT_CHANNEL_PARTY: model.ChatParty,
}
