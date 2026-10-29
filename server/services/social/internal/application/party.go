// Package application is the social service's use cases (Low-level design,
// page 11). PartyService keeps the parties (TD-19: in memory, one service
// for the whole server): up to four members, invites that expire, a leader
// who can kick and promote, and the state pushed to members after every
// change through the Notifier port, which the handler's delivery streams
// implement. ChatService cleans, limits and routes chat lines: the map
// channel to everyone on the sender's map, the party channel to the
// sender's party. Nothing is stored, so there is no repository; a restart
// forgets every party.
package application

import (
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/services/social/internal/model"
)

// Notifier is told what members must hear; the gateway implements it.
// Calls happen under the service's lock and must not block or call back.
type Notifier interface {
	// PartyChanged sends the new state to each listed member; p is nil for a
	// member who no longer has a party.
	PartyChanged(members []kernel.CharacterID, p *model.Party)
	// Invited delivers an invite to its recipient.
	Invited(to kernel.CharacterID, inv model.Invite)
}

// PartyOptions tune the service. Zero values take the defaults.
type PartyOptions struct {
	MaxMembers int
	InviteTTL  time.Duration
	Clock      func() time.Time
}

// PartyService holds every party and invite. A mutex serialises the commands
// (page 3's "one actor"): each critical section is a few map operations,
// and queries from a map tick take the same lock for microseconds.
type PartyService struct {
	opts     PartyOptions
	notifier Notifier

	mu      sync.Mutex
	known   map[kernel.CharacterID]model.Member // everyone seen online, for invites and member rows
	parties map[kernel.CharacterID]*model.Party // by member
	invites map[string]model.Invite
	seq     uint64
}

// NewPartyService makes an empty service. Notify sets where state changes go.
func NewPartyService(opts PartyOptions) *PartyService {
	if opts.MaxMembers <= 0 {
		opts.MaxMembers = model.DefaultMaxMembers
	}
	if opts.InviteTTL <= 0 {
		opts.InviteTTL = model.DefaultInviteTTL
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &PartyService{opts: opts, known: map[kernel.CharacterID]model.Member{}, parties: map[kernel.CharacterID]*model.Party{}, invites: map[string]model.Invite{}}
}

// Notify sets the notifier; nil means nobody is told.
func (s *PartyService) Notify(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// Online records a character as connected, with what the gateway knows.
// A member of a party gets their row refreshed and the party is told.
func (s *PartyService) Online(m model.Member) {
	m.Online = true
	s.upsert(m)
}

// Offline marks a character as disconnected; their party keeps them.
func (s *PartyService) Offline(id kernel.CharacterID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.known[id]
	if !ok {
		return
	}
	m.Online = false
	s.known[id] = m
	s.refresh(id, m)
}

// Update records a change the world saw: the map, level or HP of a member.
func (s *PartyService) Update(id kernel.CharacterID, mapID string, level, hp, maxHP int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.known[id]
	if !ok {
		return
	}
	m.MapID, m.Level, m.HP, m.MaxHP = mapID, level, hp, maxHP
	s.known[id] = m
	s.refresh(id, m)
}

func (s *PartyService) upsert(m model.Member) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.known[m.ID]; ok {
		m.JoinOrder = old.JoinOrder
	}
	s.known[m.ID] = m
	s.refresh(m.ID, m)
}

// refresh copies a member's row into their party and tells the party.
func (s *PartyService) refresh(id kernel.CharacterID, m model.Member) {
	p, ok := s.parties[id]
	if !ok {
		return
	}
	k := slices.IndexFunc(p.Members, func(x model.Member) bool { return x.ID == id })
	if k < 0 {
		return
	}
	m.JoinOrder = p.Members[k].JoinOrder
	p.Members[k] = m
	s.changed(p)
}

// Invite asks a character on the inviter's map to join. A character with
// no party becomes a leader when the invite is accepted.
func (s *PartyService) Invite(from, to kernel.CharacterID) (model.Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	if from == to {
		return model.Invite{}, model.ErrSelf
	}
	inviter, ok := s.known[from]
	if !ok {
		return model.Invite{}, model.ErrNotHere
	}
	if p, ok := s.parties[from]; ok {
		if p.Leader != from {
			return model.Invite{}, model.ErrNotLeader
		}
		if len(p.Members) >= s.opts.MaxMembers {
			return model.Invite{}, model.ErrFull
		}
	}
	target, ok := s.known[to]
	if !ok || !target.Online || target.MapID != inviter.MapID {
		return model.Invite{}, model.ErrNotHere
	}
	if _, ok := s.parties[to]; ok {
		return model.Invite{}, model.ErrInParty
	}
	for _, inv := range s.invites {
		if inv.From == from && inv.To == to {
			return model.Invite{}, model.ErrAlreadyAsked
		}
	}
	inv := model.Invite{ID: uuid.NewString(), From: from, FromName: inviter.Name, To: to, Expires: s.opts.Clock().Add(s.opts.InviteTTL)}
	s.invites[inv.ID] = inv
	if s.notifier != nil {
		s.notifier.Invited(to, inv)
	}
	return inv, nil
}

// Respond accepts or declines an invite addressed to who.
func (s *PartyService) Respond(who kernel.CharacterID, inviteID string, accept bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	inv, ok := s.invites[inviteID]
	if !ok || inv.To != who {
		return model.ErrInviteExpired
	}
	delete(s.invites, inviteID)
	if !accept {
		return nil
	}
	if _, ok := s.parties[who]; ok {
		return model.ErrInParty
	}
	member, ok := s.known[who]
	if !ok {
		return model.ErrNotHere
	}
	p, ok := s.parties[inv.From]
	if !ok {
		leader, ok := s.known[inv.From]
		if !ok {
			return model.ErrInviteExpired
		}
		p = &model.Party{Leader: inv.From}
		s.add(p, leader)
	}
	if len(p.Members) >= s.opts.MaxMembers {
		return model.ErrFull
	}
	s.add(p, member)
	s.changed(p)
	return nil
}

// Leave removes who from their party. The longest-standing member leads
// when the leader leaves; an empty party is gone.
func (s *PartyService) Leave(who kernel.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[who]
	if !ok {
		return model.ErrNoParty
	}
	s.remove(p, who)
	return nil
}

// Kick removes a member; only the leader may, and not themselves.
func (s *PartyService) Kick(leader, target kernel.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[leader]
	if !ok {
		return model.ErrNoParty
	}
	if p.Leader != leader {
		return model.ErrNotLeader
	}
	if target == leader || s.parties[target] != p {
		return model.ErrNotMember
	}
	s.remove(p, target)
	return nil
}

// Promote hands leadership to another member.
func (s *PartyService) Promote(leader, target kernel.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[leader]
	if !ok {
		return model.ErrNoParty
	}
	if p.Leader != leader {
		return model.ErrNotLeader
	}
	if target == leader || s.parties[target] != p {
		return model.ErrNotMember
	}
	p.Leader = target
	s.changed(p)
	return nil
}

// Of is the party of a character, copied, or false.
func (s *PartyService) Of(id kernel.CharacterID) (model.Party, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[id]
	if !ok {
		return model.Party{}, false
	}
	return model.Party{Leader: p.Leader, Members: slices.Clone(p.Members)}, true
}

// Memberships calls visit with the member IDs of every party, under the
// service's lock, so no change interleaves with the visit: a subscriber
// that takes this snapshot and then hears every PartyChanged sees each
// party's latest membership last. visit must not block or call back.
func (s *PartyService) Memberships(visit func(members []kernel.CharacterID)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[*model.Party]bool{}
	for _, p := range s.parties {
		if !seen[p] {
			seen[p] = true
			visit(ids(p))
		}
	}
}

// Members lists the IDs of a character's party, nil without one.
func (s *PartyService) Members(id kernel.CharacterID) []kernel.CharacterID {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[id]
	if !ok {
		return nil
	}
	return ids(p)
}

// SameParty reports whether two characters share a party.
func (s *PartyService) SameParty(a, b kernel.CharacterID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[a]
	return ok && s.parties[b] == p
}

func (s *PartyService) add(p *model.Party, m model.Member) {
	s.seq++
	m.JoinOrder = s.seq
	p.Members = append(p.Members, m)
	s.parties[m.ID] = p
}

func (s *PartyService) remove(p *model.Party, id kernel.CharacterID) {
	p.Members = slices.DeleteFunc(p.Members, func(m model.Member) bool { return m.ID == id })
	delete(s.parties, id)
	if s.notifier != nil {
		s.notifier.PartyChanged([]kernel.CharacterID{id}, nil)
	}
	if len(p.Members) == 0 {
		return
	}
	if p.Leader == id {
		p.Leader = slices.MinFunc(p.Members, func(a, b model.Member) int { return int(a.JoinOrder) - int(b.JoinOrder) }).ID
	}
	s.changed(p)
}

func (s *PartyService) changed(p *model.Party) {
	if s.notifier == nil {
		return
	}
	s.notifier.PartyChanged(ids(p), &model.Party{Leader: p.Leader, Members: slices.Clone(p.Members)})
}

// expire drops invites past their time.
func (s *PartyService) expire() {
	now := s.opts.Clock()
	for id, inv := range s.invites {
		if !inv.Expires.After(now) {
			delete(s.invites, id)
		}
	}
}

func ids(p *model.Party) []kernel.CharacterID {
	out := make([]kernel.CharacterID, 0, len(p.Members))
	for _, m := range p.Members {
		out = append(out, m.ID)
	}
	return out
}
