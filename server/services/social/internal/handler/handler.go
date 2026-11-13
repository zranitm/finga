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
	"finga/pkg/panics"
	"finga/services/social"
	"finga/services/social/internal/model"
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
}

// Server serves SocialService over the module's facade.
type Server struct {
	socialv1.UnimplementedSocialServiceServer
	svc     Service
	streams *Streams
	panics  *panics.Recorder // a sweep's, which runs on a timer
	// registering lets one Online, or one step of a gone gateway's sweep, run at a time, so a sweep never undoes a newer registration
	registering *semaphore.Weighted
}

// New builds the server over the facade and the streams the module notifies; rec records a sweep's panic.
func New(svc Service, streams *Streams, rec *panics.Recorder) *Server {
	return &Server{svc: svc, streams: streams, panics: rec, registering: semaphore.NewWeighted(1)}
}

// Deliver streams one gateway's party traffic.
func (s *Server) Deliver(req *socialv1.DeliverRequest, stream grpc.ServerStreamingServer[socialv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.streams.bootID}}}
	err := grpcx.ServeSubscription(stream, s.streams.deliver.Subscribe(req.GetGatewayId()), same[socialv1.DeliverResponse], first)
	s.gatewayLeft(req.GetGatewayId())
	return err
}

// gatewayLeft takes a dead gateway's players offline after gatewayGrace; a gateway that returns re-registers them.
func (s *Server) gatewayLeft(gateway string) {
	retry := &backoff.ExponentialBackOff{InitialInterval: sweepRetry, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: sweepRetryMax}
	var try func()
	try = func() {
		s.panics.Do(context.Background(), panics.FlowJob, func() {
			err := s.sweepGone(gateway)
			if err != nil && !errors.Is(err, errBack) && !errors.Is(err, kernel.ErrInternal) { // a bug in the module fails every retry
				time.AfterFunc(retry.NextBackOff(), try) // a dead gateway never comes back to finish it
			}
		}, "job", "sweep", "gateway_id", gateway)
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
	return s.svc.Offline(ctx, id, conn)
}

// WatchParties streams a snapshot, then each change since; each sets parties outright, so an overlap is harmless.
func (s *Server) WatchParties(req *socialv1.WatchPartiesRequest, stream grpc.ServerStreamingServer[socialv1.WatchPartiesResponse]) error {
	if req.GetWorldId() == "" {
		return status.Error(codes.InvalidArgument, "world_id is required")
	}
	sub := s.streams.watchers.Subscribe(req.GetWorldId())
	first := []*socialv1.WatchPartiesResponse{{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.streams.bootID}}}}
	if err := s.svc.Memberships(stream.Context(), func(party social.PartyID, members []kernel.CharacterID) {
		first = append(first, membership(members, members, party.String()))
	}); err != nil {
		sub.Close()
		return err
	}
	return grpcx.ServeSubscription(stream, sub, same[socialv1.WatchPartiesResponse], first...)
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
	if err := s.registering.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer s.registering.Release(1)
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
	if msg.Channel == social.ChannelParty {
		line := &socialv1.ChatLine{SenderId: msg.Sender.String(), SenderName: msg.SenderName, Text: msg.Text}
		for _, id := range msg.To {
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
	u := social.MemberUpdate{ID: id, MapID: req.GetMapId(), Class: kernel.Class(req.GetClass()), Level: int(req.GetLevel()), HP: int(req.GetHp()), MaxHP: int(req.GetMaxHp())}
	if err := s.svc.UpdateMember(ctx, u); err != nil {
		return nil, err
	}
	return &socialv1.UpdateMemberResponse{}, nil
}

// membership sets characters' party to members, under party's ID; empty members and ID for none.
func membership(characters, members []kernel.CharacterID, party string) *socialv1.WatchPartiesResponse {
	return &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Membership{Membership: &socialv1.Membership{
		CharacterIds: strings(characters), Members: strings(members), PartyId: party,
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
func (s *Server) Announce(_ context.Context, req *socialv1.AnnounceRequest) (*socialv1.AnnounceResponse, error) {
	a := req.GetAnnouncement()
	if a.GetKind() == nil {
		return nil, status.Error(codes.InvalidArgument, "announcement has no kind")
	}
	s.streams.Announce(a)
	return &socialv1.AnnounceResponse{}, nil
}

// same sends a message the streams already made as it is.
func same[T any](m *T) *T { return m }
