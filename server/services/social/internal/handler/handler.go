// Package handler is the social service's SocialService gRPC server and its streams, the party module's notifier.
package handler

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/social/internal/model"
)

// Parties is what the handler needs from the party module.
type Parties interface {
	Online(ctx context.Context, m model.Member) error
	Offline(ctx context.Context, id kernel.CharacterID, conn string) error
	Invite(ctx context.Context, from, to kernel.CharacterID) (model.Invite, error)
	Respond(ctx context.Context, who kernel.CharacterID, inviteID model.InviteID, accept bool) error
	Leave(ctx context.Context, who kernel.CharacterID) error
	Kick(ctx context.Context, leader, target kernel.CharacterID) error
	Promote(ctx context.Context, leader, target kernel.CharacterID) error
	Update(ctx context.Context, id kernel.CharacterID, mapID string, level, hp, maxHP int) error
	Memberships(ctx context.Context, visit func(members []kernel.CharacterID)) error
}

// Chat is what the handler needs from the chat module.
type Chat interface {
	Send(ctx context.Context, from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error)
}

// Server serves SocialService.
type Server struct {
	socialv1.UnimplementedSocialServiceServer
	parties Parties
	chat    Chat
	streams *Streams
	// registering lets one Online, or one step of a gone gateway's sweep, run at a time, so a sweep never undoes a newer registration
	registering *semaphore.Weighted
}

// New builds the server over the modules and the streams the party module notifies.
func New(parties Parties, chat Chat, streams *Streams) *Server {
	return &Server{parties: parties, chat: chat, streams: streams, registering: semaphore.NewWeighted(1)}
}

// Deliver streams one gateway's party traffic.
func (s *Server) Deliver(req *socialv1.DeliverRequest, stream grpc.ServerStreamingServer[socialv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.streams.bootID}}}
	err := grpcx.ServeSubscription(stream, s.streams.deliver.Subscribe(req.GetGatewayId()), first)
	s.gatewayLeft(req.GetGatewayId())
	return err
}

// gatewayLeft takes a dead gateway's players offline after gatewayGrace; a gateway that returns re-registers them.
func (s *Server) gatewayLeft(gateway string) {
	retry := &backoff.ExponentialBackOff{InitialInterval: sweepRetry, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: sweepRetryMax}
	var try func()
	try = func() {
		if err := s.sweepGone(gateway); err != nil && !errors.Is(err, errBack) {
			time.AfterFunc(retry.NextBackOff(), try) // a dead gateway never comes back to finish it
		}
	}
	time.AfterFunc(gatewayGrace, try)
}

// errBack ends a sweep: the gateway is back and registers its players again.
var errBack = errors.New("social: the gateway is back")

// sweepGone takes a gone gateway's players offline, until one fails or the gateway is back.
func (s *Server) sweepGone(gateway string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	for id, conn := range s.streams.on(gateway) {
		if err := s.sweep(ctx, gateway, id, conn); err != nil {
			return err
		}
	}
	return nil
}

// sweep takes one player of a gone gateway offline, unless the gateway is back.
func (s *Server) sweep(ctx context.Context, gateway string, id kernel.CharacterID, conn string) error {
	if err := s.registering.Acquire(ctx, 1); err != nil {
		return err
	}
	defer s.registering.Release(1)
	if _, back := s.streams.deliver.Get(gateway); back {
		return errBack
	}
	return s.parties.Offline(ctx, id, conn)
}

// WatchParties streams a snapshot, then each change since; each sets parties outright, so an overlap is harmless.
func (s *Server) WatchParties(req *socialv1.WatchPartiesRequest, stream grpc.ServerStreamingServer[socialv1.WatchPartiesResponse]) error {
	if req.GetWorldId() == "" {
		return status.Error(codes.InvalidArgument, "world_id is required")
	}
	sub := s.streams.watchers.Subscribe(req.GetWorldId())
	first := []*socialv1.WatchPartiesResponse{{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.streams.bootID}}}}
	if err := s.parties.Memberships(stream.Context(), func(members []kernel.CharacterID) {
		first = append(first, membership(members, members))
	}); err != nil {
		sub.Close()
		return err
	}
	return grpcx.ServeSubscription(stream, sub, first...)
}

// Online records a character as connected through a gateway.
func (s *Server) Online(ctx context.Context, req *socialv1.OnlineRequest) (*socialv1.OnlineResponse, error) {
	if req.GetGatewayId() == "" || req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id and connection_id are required")
	}
	m := req.GetMember()
	id, err := kernel.ParseCharacterID(m.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.registering.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer s.registering.Release(1)
	member := model.Member{ID: id, Name: m.GetName(), Level: int(m.GetLevel()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp()), MapID: m.GetMapId(), Connection: req.GetConnectionId(), Gateway: req.GetGatewayId()}
	if err := s.parties.Online(ctx, member); err != nil {
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
	if err := s.parties.Offline(ctx, id, req.GetConnectionId()); err != nil {
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
	inv, err := s.parties.Invite(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return &socialv1.InviteResponse{InviteId: inv.ID.String()}, nil
}

// Respond accepts or declines an invite.
func (s *Server) Respond(ctx context.Context, req *socialv1.RespondRequest) (*socialv1.RespondResponse, error) {
	who, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	invite, err := model.ParseInviteID(req.GetInviteId())
	if err != nil {
		return nil, err
	}
	if err := s.parties.Respond(ctx, who, invite, req.GetAccept()); err != nil {
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
	if err := s.parties.Leave(ctx, who); err != nil {
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
	if err := s.parties.Kick(ctx, leader, target); err != nil {
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
	if err := s.parties.Promote(ctx, leader, target); err != nil {
		return nil, err
	}
	return &socialv1.PromoteResponse{}, nil
}

var channels = map[socialv1.ChatChannel]model.Channel{
	socialv1.ChatChannel_CHAT_CHANNEL_MAP:   model.ChannelMap,
	socialv1.ChatChannel_CHAT_CHANNEL_PARTY: model.ChannelParty,
}

// Chat delivers a party line to every member; a map line goes back to the caller.
func (s *Server) Chat(ctx context.Context, req *socialv1.ChatRequest) (*socialv1.ChatResponse, error) {
	from, err := kernel.ParseCharacterID(req.GetSenderId())
	if err != nil {
		return nil, err
	}
	msg, to, err := s.chat.Send(ctx, model.Sender{ID: from, Name: req.GetSenderName()}, channels[req.GetChannel()], req.GetText())
	if err != nil {
		return nil, err
	}
	if msg.Channel == model.ChannelParty {
		line := &socialv1.ChatLine{SenderId: msg.Sender.String(), SenderName: msg.SenderName, Text: msg.Text}
		for _, id := range to {
			s.streams.send(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_ChatLine{ChatLine: line}})
		}
	}
	return &socialv1.ChatResponse{Channel: req.GetChannel(), Text: msg.Text}, nil
}

// UpdateMember records what the world saw of a member.
func (s *Server) UpdateMember(ctx context.Context, req *socialv1.UpdateMemberRequest) (*socialv1.UpdateMemberResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	if err := s.parties.Update(ctx, id, req.GetMapId(), int(req.GetLevel()), int(req.GetHp()), int(req.GetMaxHp())); err != nil {
		return nil, err
	}
	return &socialv1.UpdateMemberResponse{}, nil
}

func membership(characters, members []kernel.CharacterID) *socialv1.WatchPartiesResponse {
	return &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Membership{Membership: &socialv1.Membership{
		CharacterIds: strings(characters), Members: strings(members),
	}}}
}

func strings(ids []kernel.CharacterID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func partyOf(p *model.Party) *socialv1.Party {
	if p == nil {
		return nil
	}
	out := &socialv1.Party{LeaderId: p.Leader.String()}
	for _, m := range p.Members {
		out.Members = append(out.Members, &socialv1.Member{Id: m.ID.String(), Name: m.Name, Level: int32(m.Level), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), MapId: m.MapID, Online: m.Online})
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
