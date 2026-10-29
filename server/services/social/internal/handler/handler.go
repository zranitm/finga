// Package handler is the social service's transport layer: the
// SocialService gRPC server (api/proto/internal/social/v1) over the party
// and chat modules. It is the party module's notifier: PartyState and
// invites go out on Deliver to the gateway each member is online through
// (presence, from Online), and every membership change goes to the world
// nodes on WatchParties, so their ticks read a local copy. Party chat lines
// are delivered here too; a map line goes back to the caller.
package handler

import (
	"context"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/hub"
	"finga/services/social/internal/chat"
	"finga/services/social/internal/party"
)

// Parties is what the handler needs from the party module.
type Parties interface {
	Online(m party.Member)
	Offline(id kernel.CharacterID)
	Of(id kernel.CharacterID) (party.Party, bool)
	Invite(from, to kernel.CharacterID) (party.Invite, error)
	Respond(who kernel.CharacterID, inviteID string, accept bool) error
	Leave(who kernel.CharacterID) error
	Kick(leader, target kernel.CharacterID) error
	Promote(leader, target kernel.CharacterID) error
	Update(id kernel.CharacterID, mapID string, level, hp, maxHP int)
	Memberships(visit func(members []kernel.CharacterID))
}

// Chat is what the handler needs from the chat module.
type Chat interface {
	Send(from chat.Sender, channel chat.Channel, text string) (chat.Message, []kernel.CharacterID, error)
}

// Queue sizes: what may wait for a slow gateway or world node before its
// stream is ended and it subscribes again.
const (
	deliverQueue = 4096
	watchQueue   = 4096
)

// Server serves SocialService.
type Server struct {
	socialv1.UnimplementedSocialServiceServer
	parties  Parties
	chat     Chat
	bootID   string
	deliver  *hub.Hub[*socialv1.DeliverResponse]
	watchers *hub.Hub[*socialv1.WatchPartiesResponse]

	mu       sync.Mutex
	presence map[kernel.CharacterID]string // the gateway each online character is on
}

// New builds the server; call the party module's Notify with it. bootID is
// random per process start: a gateway that sees a new one knows the parties
// are gone.
func New(parties Parties, chat Chat, bootID string) *Server {
	return &Server{
		parties: parties, chat: chat, bootID: bootID,
		deliver:  hub.New[*socialv1.DeliverResponse](deliverQueue),
		watchers: hub.New[*socialv1.WatchPartiesResponse](watchQueue),
		presence: map[kernel.CharacterID]string{},
	}
}

// Deliver streams one gateway's party traffic.
func (s *Server) Deliver(req *socialv1.DeliverRequest, stream grpc.ServerStreamingServer[socialv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.bootID}}}
	return grpcx.ServeSubscription(stream, s.deliver.Subscribe(req.GetGatewayId()), first)
}

// WatchParties streams party membership to one world node: Subscribed, the
// current memberships, then each change. The snapshot is queued under the
// party module's lock, which every change also holds, so no change can
// land before an older snapshot.
func (s *Server) WatchParties(req *socialv1.WatchPartiesRequest, stream grpc.ServerStreamingServer[socialv1.WatchPartiesResponse]) error {
	if req.GetWorldId() == "" {
		return status.Error(codes.InvalidArgument, "world_id is required")
	}
	sub := s.watchers.Subscribe(req.GetWorldId())
	s.parties.Memberships(func(members []kernel.CharacterID) {
		sub.Send(membership(members, members))
	})
	first := &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: s.bootID}}}
	return grpcx.ServeSubscription(stream, sub, first)
}

// Online records a character as connected through a gateway.
func (s *Server) Online(_ context.Context, req *socialv1.OnlineRequest) (*socialv1.OnlineResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	m := req.GetMember()
	id, err := kernel.ParseCharacterID(m.GetId())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.presence[id] = req.GetGatewayId()
	s.mu.Unlock()
	s.parties.Online(party.Member{ID: id, Name: m.GetName(), Level: int(m.GetLevel()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp()), MapID: m.GetMapId()})
	return &socialv1.OnlineResponse{}, nil
}

// Offline marks a character as disconnected.
func (s *Server) Offline(_ context.Context, req *socialv1.OfflineRequest) (*socialv1.OfflineResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	s.parties.Offline(id)
	s.mu.Lock()
	delete(s.presence, id)
	s.mu.Unlock()
	return &socialv1.OfflineResponse{}, nil
}

// PartyOf is a character's party, if any.
func (s *Server) PartyOf(_ context.Context, req *socialv1.PartyOfRequest) (*socialv1.PartyOfResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	p, ok := s.parties.Of(id)
	if !ok {
		return &socialv1.PartyOfResponse{}, nil
	}
	return &socialv1.PartyOfResponse{Party: partyOf(&p)}, nil
}

// Invite asks a character to join the caller's party.
func (s *Server) Invite(_ context.Context, req *socialv1.InviteRequest) (*socialv1.InviteResponse, error) {
	from, to, err := pair(req.GetCharacterId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}
	inv, err := s.parties.Invite(from, to)
	if err != nil {
		return nil, err
	}
	return &socialv1.InviteResponse{InviteId: inv.ID}, nil
}

// Respond accepts or declines an invite.
func (s *Server) Respond(_ context.Context, req *socialv1.RespondRequest) (*socialv1.RespondResponse, error) {
	who, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return &socialv1.RespondResponse{}, s.parties.Respond(who, req.GetInviteId(), req.GetAccept())
}

// LeaveParty removes the caller from its party.
func (s *Server) LeaveParty(_ context.Context, req *socialv1.LeavePartyRequest) (*socialv1.LeavePartyResponse, error) {
	who, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	return &socialv1.LeavePartyResponse{}, s.parties.Leave(who)
}

// Kick removes a member.
func (s *Server) Kick(_ context.Context, req *socialv1.KickRequest) (*socialv1.KickResponse, error) {
	leader, target, err := pair(req.GetCharacterId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}
	return &socialv1.KickResponse{}, s.parties.Kick(leader, target)
}

// Promote hands leadership to another member.
func (s *Server) Promote(_ context.Context, req *socialv1.PromoteRequest) (*socialv1.PromoteResponse, error) {
	leader, target, err := pair(req.GetCharacterId(), req.GetTargetId())
	if err != nil {
		return nil, err
	}
	return &socialv1.PromoteResponse{}, s.parties.Promote(leader, target)
}

var channels = map[socialv1.ChatChannel]chat.Channel{
	socialv1.ChatChannel_CHAT_CHANNEL_MAP:   chat.Map,
	socialv1.ChatChannel_CHAT_CHANNEL_PARTY: chat.Party,
}

// Chat cleans and limits a line; a party line is delivered to every member,
// a map line goes back to the caller.
func (s *Server) Chat(_ context.Context, req *socialv1.ChatRequest) (*socialv1.ChatResponse, error) {
	from, err := kernel.ParseCharacterID(req.GetSenderId())
	if err != nil {
		return nil, err
	}
	msg, to, err := s.chat.Send(chat.Sender{ID: from, Name: req.GetSenderName()}, channels[req.GetChannel()], req.GetText())
	if err != nil {
		return nil, err
	}
	if msg.Channel == chat.Party {
		line := &socialv1.ChatLine{SenderId: msg.Sender.String(), SenderName: msg.SenderName, Text: msg.Text}
		for _, id := range to {
			s.send(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_ChatLine{ChatLine: line}})
		}
	}
	return &socialv1.ChatResponse{Channel: req.GetChannel(), Text: msg.Text}, nil
}

// UpdateMember records what the world saw of a member.
func (s *Server) UpdateMember(_ context.Context, req *socialv1.UpdateMemberRequest) (*socialv1.UpdateMemberResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	s.parties.Update(id, req.GetMapId(), int(req.GetLevel()), int(req.GetHp()), int(req.GetMaxHp()))
	return &socialv1.UpdateMemberResponse{}, nil
}

// PartyChanged implements party.Notifier: each listed member's gateway gets
// the new state, and every world node the new membership. It runs under
// the party module's lock and never blocks.
func (s *Server) PartyChanged(members []kernel.CharacterID, p *party.Party) {
	state := &socialv1.PartyState{Party: partyOf(p)}
	for _, id := range members {
		s.send(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyState{PartyState: state}})
	}
	var now []kernel.CharacterID
	if p != nil {
		for _, m := range p.Members {
			now = append(now, m.ID)
		}
	}
	s.watchers.Broadcast(membership(members, now))
}

// Invited implements party.Notifier: the invitee's gateway gets it.
func (s *Server) Invited(to kernel.CharacterID, inv party.Invite) {
	s.send(to, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{
		InviteId: inv.ID, FromId: inv.From.String(), FromName: inv.FromName,
	}}})
}

// send delivers to the gateway a character is online through, if any; a
// character offline, or whose gateway has no stream, misses it.
func (s *Server) send(to kernel.CharacterID, ev *socialv1.DeliverResponse) {
	s.mu.Lock()
	gateway, ok := s.presence[to]
	s.mu.Unlock()
	if !ok {
		return
	}
	if sub, ok := s.deliver.Get(gateway); ok {
		ev.CharacterId = to.String()
		sub.Send(ev)
	}
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

func partyOf(p *party.Party) *socialv1.Party {
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
