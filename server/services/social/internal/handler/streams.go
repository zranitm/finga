package handler

import (
	"sync"

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

// Streams are the gateways' delivery streams and the worlds' membership watch, with the gateway each
// character is online on; they are the party module's notifier.
type Streams struct {
	bootID   string
	deliver  *hub.Hub[*socialv1.DeliverResponse]
	watchers *hub.Hub[*socialv1.WatchPartiesResponse]

	mu       sync.Mutex
	presence map[kernel.CharacterID]string
}

var _ application.Notifier = (*Streams)(nil)

// NewStreams makes the streams; a new bootID tells gateways the parties are gone.
func NewStreams(bootID string) *Streams {
	return &Streams{
		bootID:   bootID,
		deliver:  hub.New[*socialv1.DeliverResponse](deliverQueue),
		watchers: hub.New[*socialv1.WatchPartiesResponse](watchQueue),
		presence: map[kernel.CharacterID]string{},
	}
}

// PartyChanged runs under the party lock and never blocks.
func (s *Streams) PartyChanged(members []kernel.CharacterID, p *model.Party) {
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

// Invited tells the invitee.
func (s *Streams) Invited(to kernel.CharacterID, inv model.Invite) {
	s.send(to, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{
		InviteId: inv.ID.String(), FromId: inv.From.String(), FromName: inv.FromName,
	}}})
}

func (s *Streams) online(id kernel.CharacterID, gateway string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presence[id] = gateway
}

func (s *Streams) offline(id kernel.CharacterID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.presence, id)
}

// send delivers to the character's gateway, if it is online there with a stream.
func (s *Streams) send(to kernel.CharacterID, ev *socialv1.DeliverResponse) {
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
