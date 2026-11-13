// Package handler is the social service's SocialService gRPC server.
package handler

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/hub"
	"finga/services/social"
)

// Service is the social module's facade, as the handler calls it.
type Service interface {
	Online(ctx context.Context, p social.Presence) error
	Offline(ctx context.Context, id kernel.CharacterID, conn string) error
	Invite(ctx context.Context, from, to kernel.CharacterID) (social.InviteID, error)
	Respond(ctx context.Context, who kernel.CharacterID, invite social.InviteID, accept bool) error
	LeaveParty(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
	Chat(ctx context.Context, l social.Line) (social.Message, error)
	UpdateMember(ctx context.Context, u social.MemberUpdate) error
	Memberships(ctx context.Context, visit func(party social.PartyID, members []kernel.CharacterID)) error
	Announce(ctx context.Context, a social.Announcement) error
	Subscribe(gateway string) *hub.Sub[social.Delivery]
	SubscribeMemberships(world string) *hub.Sub[social.MembershipEvent]
	GatewayLeft(gateway string)
}

// Server serves SocialService over the module's facade.
type Server struct {
	socialv1.UnimplementedSocialServiceServer
	svc    Service
	bootID string
}

// New builds the server over the facade; a per-process bootID tells gateways and worlds the parties are gone.
func New(svc Service, bootID string) *Server {
	return &Server{svc: svc, bootID: bootID}
}

// Deliver streams one gateway's party traffic.
func (s *Server) Deliver(req *socialv1.DeliverRequest, stream grpc.ServerStreamingServer[socialv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.bootID}}}
	err := grpcx.ServeSubscription(stream, s.svc.Subscribe(req.GetGatewayId()), deliveryMessage, first)
	s.svc.GatewayLeft(req.GetGatewayId())
	return err
}

// WatchParties streams a snapshot, then each change since; each sets parties outright, so an overlap is harmless.
func (s *Server) WatchParties(req *socialv1.WatchPartiesRequest, stream grpc.ServerStreamingServer[socialv1.WatchPartiesResponse]) error {
	if req.GetWorldId() == "" {
		return status.Error(codes.InvalidArgument, "world_id is required")
	}
	sub := s.svc.SubscribeMemberships(req.GetWorldId())
	first := []*socialv1.WatchPartiesResponse{{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.bootID}}}}
	if err := s.svc.Memberships(stream.Context(), func(party social.PartyID, members []kernel.CharacterID) {
		first = append(first, membershipMessage(social.Membership{Characters: members, Members: members, Party: party}))
	}); err != nil {
		sub.Close()
		return err
	}
	return grpcx.ServeSubscription(stream, sub, membershipMessage, first...)
}

// Online records a character as connected through a gateway.
func (s *Server) Online(ctx context.Context, req *socialv1.OnlineRequest) (*socialv1.OnlineResponse, error) {
	if req.GetGatewayId() == "" || req.GetConnectionId() == "" || req.GetJoinSeq() == 0 {
		return nil, status.Error(codes.InvalidArgument, "gateway_id, connection_id and join_seq are required")
	}
	m := req.GetMember()
	id, err := kernel.ParseCharacterID(m.GetId())
	if err != nil {
		return nil, err
	}
	member := social.Member{ID: id, Name: m.GetName(), Class: kernel.Class(m.GetClass()), Level: int(m.GetLevel()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp()), MapID: m.GetMapId()}
	if err := s.svc.Online(ctx, social.Presence{Member: member, Gateway: req.GetGatewayId(), Connection: req.GetConnectionId(), JoinSeq: req.GetJoinSeq()}); err != nil {
		return nil, err
	}
	return &socialv1.OnlineResponse{}, nil
}

// Offline marks a character as disconnected, unless they reconnected through another connection since.
func (s *Server) Offline(ctx context.Context, req *socialv1.OfflineRequest) (*socialv1.OfflineResponse, error) {
	if req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "connection_id is required")
	}
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.Offline(ctx, id, req.GetConnectionId()); err != nil {
		return nil, err
	}
	return &socialv1.OfflineResponse{}, nil
}

// Invite asks a character to join the caller's party.
func (s *Server) Invite(ctx context.Context, req *socialv1.InviteRequest) (*socialv1.InviteResponse, error) {
	from, to, err := pair(req.GetCharacterId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}
	invite, err := s.svc.Invite(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return &socialv1.InviteResponse{InviteId: invite.String()}, nil
}

// Respond accepts or declines an invite.
func (s *Server) Respond(ctx context.Context, req *socialv1.RespondRequest) (*socialv1.RespondResponse, error) {
	who, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	invite, err := social.ParseInviteID(req.GetInviteId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.Respond(ctx, who, invite, req.GetAccept()); err != nil {
		return nil, err
	}
	return &socialv1.RespondResponse{}, nil
}

// LeaveParty removes the caller from its party.
func (s *Server) LeaveParty(ctx context.Context, req *socialv1.LeavePartyRequest) (*socialv1.LeavePartyResponse, error) {
	who, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.LeaveParty(ctx, who); err != nil {
		return nil, err
	}
	return &socialv1.LeavePartyResponse{}, nil
}

// Kick removes a member.
func (s *Server) Kick(ctx context.Context, req *socialv1.KickRequest) (*socialv1.KickResponse, error) {
	leader, target, err := pair(req.GetCharacterId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.Kick(ctx, leader, target); err != nil {
		return nil, err
	}
	return &socialv1.KickResponse{}, nil
}

// Promote hands leadership to another member.
func (s *Server) Promote(ctx context.Context, req *socialv1.PromoteRequest) (*socialv1.PromoteResponse, error) {
	leader, target, err := pair(req.GetCharacterId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.Promote(ctx, leader, target); err != nil {
		return nil, err
	}
	return &socialv1.PromoteResponse{}, nil
}

var channels = map[socialv1.ChatChannel]social.Channel{
	socialv1.ChatChannel_CHAT_CHANNEL_MAP:   social.ChannelMap,
	socialv1.ChatChannel_CHAT_CHANNEL_PARTY: social.ChannelParty,
}

// Chat delivers a party line to every member; a map line goes back to the caller.
func (s *Server) Chat(ctx context.Context, req *socialv1.ChatRequest) (*socialv1.ChatResponse, error) {
	from, err := kernel.ParseCharacterID(req.GetSenderId())
	if err != nil {
		return nil, err
	}
	msg, err := s.svc.Chat(ctx, social.Line{From: from, FromName: req.GetSenderName(), Channel: channels[req.GetChannel()], Text: req.GetText()})
	if err != nil {
		return nil, err
	}
	return &socialv1.ChatResponse{Channel: req.GetChannel(), Text: msg.Text}, nil
}

// UpdateMember records what the world saw of a member.
func (s *Server) UpdateMember(ctx context.Context, req *socialv1.UpdateMemberRequest) (*socialv1.UpdateMemberResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	u := social.MemberUpdate{ID: id, MapID: req.GetMapId(), Class: kernel.Class(req.GetClass()), Level: int(req.GetLevel()), HP: int(req.GetHp()), MaxHP: int(req.GetMaxHp())}
	if err := s.svc.UpdateMember(ctx, u); err != nil {
		return nil, err
	}
	return &socialv1.UpdateMemberResponse{}, nil
}

// deliveryMessage is d as its gateway hears it, for the player it names; an announcement names none.
func deliveryMessage(d social.Delivery) *socialv1.DeliverResponse {
	switch e := d.(type) {
	case social.PartyState:
		return &socialv1.DeliverResponse{CharacterId: e.To.String(), Event: &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{Party: partyOf(e.Party)}}}
	case social.Invited:
		return &socialv1.DeliverResponse{CharacterId: e.To.String(), Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{
			InviteId: e.Invite.String(), FromId: e.From.String(), FromName: e.FromName,
		}}}
	case social.ChatLine:
		return &socialv1.DeliverResponse{CharacterId: e.To.String(), Event: &socialv1.DeliverResponse_ChatLine{ChatLine: &socialv1.ChatLine{
			SenderId: e.Sender.String(), SenderName: e.SenderName, Text: e.Text,
		}}}
	case social.BossAppeared:
		return &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Announcement{Announcement: &socialv1.Announcement{
			Kind: &socialv1.Announcement_BossAppeared{BossAppeared: &socialv1.BossAppeared{Boss: e.Boss, MapId: e.MapID}},
		}}}
	}
	return nil
}

// membershipMessage is e as a world hears it: characters' party set to members under the party's ID, empty for none; nil for no membership.
func membershipMessage(e social.MembershipEvent) *socialv1.WatchPartiesResponse {
	m, ok := e.(social.Membership)
	if !ok {
		return nil
	}
	party := ""
	if !m.Party.IsZero() {
		party = m.Party.String()
	}
	return &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Membership{Membership: &socialv1.Membership{
		CharacterIds: strings(m.Characters), Members: strings(m.Members), PartyId: party,
	}}}
}

func strings(ids []kernel.CharacterID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func partyOf(p *social.Party) *socialv1.Party {
	if p == nil {
		return nil
	}
	out := &socialv1.Party{LeaderId: p.Leader.String()}
	for _, m := range p.Members {
		out.Members = append(out.Members, &socialv1.Member{Id: m.ID.String(), Name: m.Name, Class: string(m.Class), Level: int32(m.Level), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), MapId: m.MapID, Online: m.Online})
	}
	return out
}

func pair(a, b string) (kernel.CharacterID, kernel.CharacterID, error) {
	first, err := kernel.ParseCharacterID(a)
	if err != nil {
		return first, first, err
	}
	second, err := kernel.ParseCharacterID(b)
	return first, second, err
}

// Announce delivers news to every character online; one of no kind is refused.
func (s *Server) Announce(ctx context.Context, req *socialv1.AnnounceRequest) (*socialv1.AnnounceResponse, error) {
	a, ok := announcementOf(req.GetAnnouncement())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "announcement has no kind")
	}
	if err := s.svc.Announce(ctx, a); err != nil {
		return nil, err
	}
	return &socialv1.AnnounceResponse{}, nil
}

func announcementOf(a *socialv1.Announcement) (social.Announcement, bool) {
	if b := a.GetBossAppeared(); b != nil {
		return social.BossAppeared{Boss: b.GetBoss(), MapID: b.GetMapId()}, true
	}
	return nil, false
}
