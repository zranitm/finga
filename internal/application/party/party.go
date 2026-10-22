// Package party keeps the parties (TD-19: in memory, one service for the
// whole server): up to four members, invites that expire, a leader who can
// kick and promote, and the state pushed to members after every change.
// It has no repository; a restart forgets every party.
package party

import (
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"finga/internal/model"
	"finga/pkg/errs"
)

// Defaults (page 5).
const (
	DefaultMaxMembers = 4
	DefaultInviteTTL  = 30 * time.Second
)

// Member is a party member as the others see them. The service learns
// these from the gateway (online, offline) and the world (map, level, HP).
type Member struct {
	ID     model.CharacterID
	Name   string
	Level  int
	HP     int
	MaxHP  int
	MapID  string
	Online bool
	since  uint64 // join order: the longest-standing member has the lowest
}

// Party is one party: the leader and the members in join order.
type Party struct {
	Leader  model.CharacterID
	Members []Member
}

// Invite is a pending invitation.
type Invite struct {
	ID       string
	From     model.CharacterID
	FromName string
	To       model.CharacterID
	Expires  time.Time
}

// Notifier is told what members must hear; the gateway implements it.
// Calls happen under the service's lock and must not block or call back.
type Notifier interface {
	// PartyChanged sends the new state to each listed member; p is nil for a
	// member who no longer has a party.
	PartyChanged(members []model.CharacterID, p *Party)
	// Invited delivers an invite to its recipient.
	Invited(to model.CharacterID, inv Invite)
}

// Options tune the service. Zero values take the defaults.
type Options struct {
	MaxMembers int
	InviteTTL  time.Duration
	Clock      func() time.Time
}

// Service holds every party and invite. A mutex serialises the commands
// (page 3's "one actor"): each critical section is a few map operations,
// and queries from a map tick take the same lock for microseconds.
type Service struct {
	opts     Options
	notifier Notifier

	mu      sync.Mutex
	known   map[model.CharacterID]Member // everyone seen online, for invites and member rows
	parties map[model.CharacterID]*Party // by member
	invites map[string]Invite
	seq     uint64
}

// NewService makes an empty service. Notify sets where state changes go.
func NewService(opts Options) *Service {
	if opts.MaxMembers <= 0 {
		opts.MaxMembers = DefaultMaxMembers
	}
	if opts.InviteTTL <= 0 {
		opts.InviteTTL = DefaultInviteTTL
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Service{opts: opts, known: map[model.CharacterID]Member{}, parties: map[model.CharacterID]*Party{}, invites: map[string]Invite{}}
}

// Notify sets the notifier; nil means nobody is told.
func (s *Service) Notify(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// Errors the commands return.
var (
	ErrNotLeader     = errs.New(errs.Conflict, "only the party leader can do that")
	ErrFull          = errs.New(errs.Conflict, "the party is full")
	ErrInParty       = errs.New(errs.Conflict, "already in a party")
	ErrNoParty       = errs.New(errs.NotFound, "not in a party")
	ErrNotMember     = errs.New(errs.NotFound, "not a member of your party")
	ErrNotHere       = errs.New(errs.NotFound, "that character is not on your map")
	ErrAlreadyAsked  = errs.New(errs.Conflict, "already invited")
	ErrInviteExpired = errs.New(errs.NotFound, "the invite has expired")
	ErrSelf          = errs.Invalid("you cannot invite yourself")
)

// Online records a character as connected, with what the gateway knows.
// A member of a party gets their row refreshed and the party is told.
func (s *Service) Online(m Member) {
	m.Online = true
	s.upsert(m)
}

// Offline marks a character as disconnected; their party keeps them.
func (s *Service) Offline(id model.CharacterID) {
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
func (s *Service) Update(id model.CharacterID, mapID string, level, hp, maxHP int) {
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

func (s *Service) upsert(m Member) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.known[m.ID]; ok {
		m.since = old.since
	}
	s.known[m.ID] = m
	s.refresh(m.ID, m)
}

// refresh copies a member's row into their party and tells the party.
func (s *Service) refresh(id model.CharacterID, m Member) {
	p, ok := s.parties[id]
	if !ok {
		return
	}
	k := slices.IndexFunc(p.Members, func(x Member) bool { return x.ID == id })
	if k < 0 {
		return
	}
	m.since = p.Members[k].since
	p.Members[k] = m
	s.changed(p)
}

// Invite asks a character on the inviter's map to join. A character with
// no party becomes a leader when the invite is accepted.
func (s *Service) Invite(from, to model.CharacterID) (Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	if from == to {
		return Invite{}, ErrSelf
	}
	inviter, ok := s.known[from]
	if !ok {
		return Invite{}, ErrNotHere
	}
	if p, ok := s.parties[from]; ok {
		if p.Leader != from {
			return Invite{}, ErrNotLeader
		}
		if len(p.Members) >= s.opts.MaxMembers {
			return Invite{}, ErrFull
		}
	}
	target, ok := s.known[to]
	if !ok || !target.Online || target.MapID != inviter.MapID {
		return Invite{}, ErrNotHere
	}
	if _, ok := s.parties[to]; ok {
		return Invite{}, ErrInParty
	}
	for _, inv := range s.invites {
		if inv.From == from && inv.To == to {
			return Invite{}, ErrAlreadyAsked
		}
	}
	inv := Invite{ID: uuid.NewString(), From: from, FromName: inviter.Name, To: to, Expires: s.opts.Clock().Add(s.opts.InviteTTL)}
	s.invites[inv.ID] = inv
	if s.notifier != nil {
		s.notifier.Invited(to, inv)
	}
	return inv, nil
}

// Respond accepts or declines an invite addressed to who.
func (s *Service) Respond(who model.CharacterID, inviteID string, accept bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	inv, ok := s.invites[inviteID]
	if !ok || inv.To != who {
		return ErrInviteExpired
	}
	delete(s.invites, inviteID)
	if !accept {
		return nil
	}
	if _, ok := s.parties[who]; ok {
		return ErrInParty
	}
	member, ok := s.known[who]
	if !ok {
		return ErrNotHere
	}
	p, ok := s.parties[inv.From]
	if !ok {
		leader, ok := s.known[inv.From]
		if !ok {
			return ErrInviteExpired
		}
		p = &Party{Leader: inv.From}
		s.add(p, leader)
	}
	if len(p.Members) >= s.opts.MaxMembers {
		return ErrFull
	}
	s.add(p, member)
	s.changed(p)
	return nil
}

// Leave removes who from their party. The longest-standing member leads
// when the leader leaves; an empty party is gone.
func (s *Service) Leave(who model.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[who]
	if !ok {
		return ErrNoParty
	}
	s.remove(p, who)
	return nil
}

// Kick removes a member; only the leader may, and not themselves.
func (s *Service) Kick(leader, target model.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[leader]
	if !ok {
		return ErrNoParty
	}
	if p.Leader != leader {
		return ErrNotLeader
	}
	if target == leader || s.parties[target] != p {
		return ErrNotMember
	}
	s.remove(p, target)
	return nil
}

// Promote hands leadership to another member.
func (s *Service) Promote(leader, target model.CharacterID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[leader]
	if !ok {
		return ErrNoParty
	}
	if p.Leader != leader {
		return ErrNotLeader
	}
	if target == leader || s.parties[target] != p {
		return ErrNotMember
	}
	p.Leader = target
	s.changed(p)
	return nil
}

// Of is the party of a character, copied, or false.
func (s *Service) Of(id model.CharacterID) (Party, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[id]
	if !ok {
		return Party{}, false
	}
	return Party{Leader: p.Leader, Members: slices.Clone(p.Members)}, true
}

// Members lists the IDs of a character's party, nil without one.
func (s *Service) Members(id model.CharacterID) []model.CharacterID {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[id]
	if !ok {
		return nil
	}
	return ids(p)
}

// SameParty reports whether two characters share a party.
func (s *Service) SameParty(a, b model.CharacterID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[a]
	return ok && s.parties[b] == p
}

func (s *Service) add(p *Party, m Member) {
	s.seq++
	m.since = s.seq
	p.Members = append(p.Members, m)
	s.parties[m.ID] = p
}

func (s *Service) remove(p *Party, id model.CharacterID) {
	p.Members = slices.DeleteFunc(p.Members, func(m Member) bool { return m.ID == id })
	delete(s.parties, id)
	if s.notifier != nil {
		s.notifier.PartyChanged([]model.CharacterID{id}, nil)
	}
	if len(p.Members) == 0 {
		return
	}
	if p.Leader == id {
		p.Leader = slices.MinFunc(p.Members, func(a, b Member) int { return int(a.since) - int(b.since) }).ID
	}
	s.changed(p)
}

func (s *Service) changed(p *Party) {
	if s.notifier == nil {
		return
	}
	s.notifier.PartyChanged(ids(p), &Party{Leader: p.Leader, Members: slices.Clone(p.Members)})
}

// expire drops invites past their time.
func (s *Service) expire() {
	now := s.opts.Clock()
	for id, inv := range s.invites {
		if !inv.Expires.After(now) {
			delete(s.invites, id)
		}
	}
}

func ids(p *Party) []model.CharacterID {
	out := make([]model.CharacterID, 0, len(p.Members))
	for _, m := range p.Members {
		out = append(out, m.ID)
	}
	return out
}
