package clients

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/grpcx"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// Social is the application's Social over the social service.
type Social struct {
	c   socialv1.SocialServiceClient
	log *slog.Logger
}

// NewSocial adapts the social client; log hears the stream's breaks.
func NewSocial(c socialv1.SocialServiceClient, log *slog.Logger) Social {
	return Social{c: c, log: log}
}

// PartyOf is the character's party, nil when it has none.
func (s Social) PartyOf(ctx context.Context, character kernel.CharacterID) (*protocol.PartyView, error) {
	res, err := s.c.PartyOf(ctx, &socialv1.PartyOfRequest{CharacterId: character.String()})
	return partyView(res.GetParty()), errmap.Wrap(err)
}

// Online registers a player connected through this gateway.
func (s Social) Online(ctx context.Context, gateway string, p model.Player, mapID string) error {
	member := &socialv1.Member{Id: p.ID.String(), Name: p.Name, Level: int32(p.Level), Hp: int32(p.HP), MaxHp: int32(p.MaxHP), MapId: mapID}
	_, err := s.c.Online(ctx, &socialv1.OnlineRequest{GatewayId: gateway, Member: member})
	return errmap.Wrap(err)
}

// Offline tells the service the player disconnected.
func (s Social) Offline(ctx context.Context, character kernel.CharacterID) error {
	_, err := s.c.Offline(ctx, &socialv1.OfflineRequest{CharacterId: character.String()})
	return errmap.Wrap(err)
}

// chatChannels are the client's channels in the contract; anything else is
// unspecified, which the social service refuses.
var chatChannels = map[protocol.ChatChannel]socialv1.ChatChannel{
	protocol.ChatMap:   socialv1.ChatChannel_CHAT_CHANNEL_MAP,
	protocol.ChatParty: socialv1.ChatChannel_CHAT_CHANNEL_PARTY,
}

// Chat cleans and limits a line.
func (s Social) Chat(ctx context.Context, sender model.Player, channel protocol.ChatChannel, text string) (model.ChatLine, error) {
	res, err := s.c.Chat(ctx, &socialv1.ChatRequest{SenderId: sender.ID.String(), SenderName: sender.Name, Channel: chatChannels[channel], Text: text})
	if err != nil {
		return model.ChatLine{}, errmap.Wrap(err)
	}
	line := model.ChatLine{Channel: protocol.ChatParty, Text: res.GetText()}
	if res.GetChannel() == socialv1.ChatChannel_CHAT_CHANNEL_MAP {
		line.Channel = protocol.ChatMap
	}
	return line, nil
}

// Invite invites a character to the inviter's party.
func (s Social) Invite(ctx context.Context, from, to kernel.CharacterID) error {
	_, err := s.c.Invite(ctx, &socialv1.InviteRequest{CharacterId: from.String(), TargetId: to.String()})
	return errmap.Wrap(err)
}

// Respond accepts or declines an invite.
func (s Social) Respond(ctx context.Context, who kernel.CharacterID, inviteID string, accept bool) error {
	_, err := s.c.Respond(ctx, &socialv1.RespondRequest{CharacterId: who.String(), InviteId: inviteID, Accept: accept})
	return errmap.Wrap(err)
}

// Leave leaves the party.
func (s Social) Leave(ctx context.Context, who kernel.CharacterID) error {
	_, err := s.c.LeaveParty(ctx, &socialv1.LeavePartyRequest{CharacterId: who.String()})
	return errmap.Wrap(err)
}

// Kick removes a member; the leader's command.
func (s Social) Kick(ctx context.Context, leader, target kernel.CharacterID) error {
	_, err := s.c.Kick(ctx, &socialv1.KickRequest{CharacterId: leader.String(), TargetId: target.String()})
	return errmap.Wrap(err)
}

// Promote hands the lead to a member; the leader's command.
func (s Social) Promote(ctx context.Context, leader, target kernel.CharacterID) error {
	_, err := s.c.Promote(ctx, &socialv1.PromoteRequest{CharacterId: leader.String(), TargetId: target.String()})
	return errmap.Wrap(err)
}

// FollowDeliveries follows this gateway's party and chat stream until ctx
// ends, turning each event into what its player sees.
func (s Social) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.SocialEvent)) {
	grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[socialv1.DeliverResponse], error) {
		return s.c.Deliver(ctx, &socialv1.DeliverRequest{GatewayId: gateway})
	}, func(m *socialv1.DeliverResponse) {
		if sub := m.GetSubscribed(); sub != nil {
			deliver(model.SocialEvent{Subscribed: true, BootID: sub.GetBootId()})
			return
		}
		id, err := kernel.ParseCharacterID(m.GetCharacterId())
		if err != nil {
			return
		}
		var data any
		switch {
		case m.GetPartyState() != nil:
			data = protocol.PartyState{Party: partyView(m.GetPartyState().GetParty())}
		case m.GetPartyInvited() != nil:
			inv := m.GetPartyInvited()
			data = protocol.PartyInvited{InviteID: inv.GetInviteId(), From: inv.GetFromId(), FromName: inv.GetFromName()}
		case m.GetChatLine() != nil:
			line := m.GetChatLine()
			data = protocol.ChatMessage{Channel: protocol.ChatParty, Sender: line.GetSenderId(), SenderName: line.GetSenderName(), Text: line.GetText()}
		default:
			return
		}
		deliver(model.SocialEvent{Character: id, Message: model.Message{Data: data}})
	}, grpcx.FollowOptions{OnBreak: func(err error) {
		s.log.WarnContext(ctx, "social stream broke; subscribing again", "err", err)
	}})
}

func partyView(p *socialv1.Party) *protocol.PartyView {
	if p == nil {
		return nil
	}
	v := &protocol.PartyView{Leader: p.GetLeaderId(), Members: make([]protocol.PartyMember, 0, len(p.GetMembers()))}
	for _, m := range p.GetMembers() {
		v.Members = append(v.Members, protocol.PartyMember{CharacterID: m.GetId(), Name: m.GetName(), Level: int(m.GetLevel()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp()), MapID: m.GetMapId(), Online: m.GetOnline()})
	}
	return v
}
