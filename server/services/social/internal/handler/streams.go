package handler

import (
	"maps"
	"sync"
	"time"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
)

// Queue sizes for a slow gateway or world node before its stream is dropped.
const (
	deliverQueue = 4096
	watchQueue   = 4096
)

// A gateway's players go offline once its stream has stayed gone gatewayGrace; marking them takes up to sweepTimeout, and a sweep cut short tries again after sweepRetry, backing off to sweepRetryMax.
const (
	gatewayGrace  = 30 * time.Second
	sweepTimeout  = 10 * time.Second
	sweepRetry    = 5 * time.Second
	sweepRetryMax = time.Minute
)

// Streams are the gateway delivery and world membership streams, with each character's gateway: the party notifier.
type Streams struct {
	bootID   string
	deliver  *hub.Hub[*socialv1.DeliverResponse]
	watchers *hub.Hub[*socialv1.WatchPartiesResponse]

	mu       sync.Mutex
	presence map[kernel.CharacterID]presence
}

// presence is where a character is online: the gateway and the connection.
type presence struct{ gateway, conn string }

var _ application.Notifier = (*Streams)(nil)

// NewStreams makes the streams; a new bootID tells gateways the parties are gone.
func NewStreams(bootID string) *Streams {
	return &Streams{
		bootID:   bootID,
		deliver:  hub.New[*socialv1.DeliverResponse](deliverQueue),
		watchers: hub.New[*socialv1.WatchPartiesResponse](watchQueue),
		presence: map[kernel.CharacterID]presence{},
	}
}

// PartyChanged runs under the party lock and never blocks.
func (s *Streams) PartyChanged(members []kernel.CharacterID, p *model.Party) {
	state := &socialv1.PartyState{Party: partyOf(p)}
	for _, id := range members {
		s.send(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyState{PartyState: state}})
	}
	var now []kernel.CharacterID
	party := ""
	if p != nil {
		for _, m := range p.Members {
			now = append(now, m.ID)
		}
		party = p.ID.String()
	}
	s.watchers.Broadcast(membership(members, now, party))
}

// Alone sends the member an empty PartyState; the worlds already hold them in no party.
func (s *Streams) Alone(id kernel.CharacterID) {
	s.send(id, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{}}})
}

// Invited tells the invitee.
func (s *Streams) Invited(to kernel.CharacterID, inv model.Invite) {
	s.send(to, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{
		InviteId: inv.ID.String(), FromId: inv.From.String(), FromName: inv.FromName,
	}}})
}

// Reachable routes the character's deliveries to the gateway; it runs under the party lock, which decided the member is online there.
func (s *Streams) Reachable(id kernel.CharacterID, gateway, conn string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presence[id] = presence{gateway, conn}
}

// Unreachable forgets where the character is, if still through conn.
func (s *Streams) Unreachable(id kernel.CharacterID, conn string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.presence[id].conn == conn {
		delete(s.presence, id)
	}
}

// Announce sends a to every character online, through each one's gateway; it never blocks.
func (s *Streams) Announce(a *socialv1.Announcement) {
	s.mu.Lock()
	online := maps.Clone(s.presence)
	s.mu.Unlock()
	for id, p := range online {
		if sub, ok := s.deliver.Get(p.gateway); ok {
			sub.Send(&socialv1.DeliverResponse{CharacterId: id.String(), Event: &socialv1.DeliverResponse_Announcement{Announcement: a}})
		}
	}
}

// on is each character online through the gateway, with its connection.
func (s *Streams) on(gateway string) map[kernel.CharacterID]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[kernel.CharacterID]string{}
	for id, p := range s.presence {
		if p.gateway == gateway {
			out[id] = p.conn
		}
	}
	return out
}

// send delivers to the character's gateway, if it is online there with a stream.
func (s *Streams) send(to kernel.CharacterID, ev *socialv1.DeliverResponse) {
	s.mu.Lock()
	p, ok := s.presence[to]
	s.mu.Unlock()
	if !ok {
		return
	}
	if sub, ok := s.deliver.Get(p.gateway); ok {
		ev.CharacterId = to.String()
		sub.Send(ev)
	}
}
