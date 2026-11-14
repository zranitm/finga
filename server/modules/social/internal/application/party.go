// Package application holds the social service's party and chat rules.
package application

import (
	"context"
	"slices"
	"sync"
	"time"

	"finga/internal/kernel"
	"finga/modules/social/internal/model"
)

// PartyService runs the party rules over a PartyStore.
type PartyService struct {
	store PartyStore

	notifier Notifier

	// mu orders each change with its notifications, so members never hear an older state last.
	mu sync.Mutex
}

// PartyDeps are what the party service is built over.
type PartyDeps struct {
	Store    PartyStore
	Notifier Notifier // hears every change members must know of
}

// NewPartyService makes the service over deps.
func NewPartyService(deps PartyDeps) *PartyService {
	return &PartyService{store: deps.Store, notifier: deps.Notifier}
}

// Online records a character as connected, routes their deliveries there and tells them their party, keeping what the world reported through the same connection; one from an older join than theirs is an ended tab's late registration and changes nothing, deliveries included.
func (s *PartyService) Online(ctx context.Context, m model.Member) error {
	m.Online = true
	return s.update(ctx, func(tx PartyTx, n *notes) error {
		if old, ok := tx.Member(m.ID); ok {
			if m.JoinSeq < old.JoinSeq {
				return nil
			}
			m.JoinOrder = old.JoinOrder
			if old.Connection == m.Connection || old.Connection == "" && time.Since(old.ReportedAt) < reportKept {
				m.Class, m.Level, m.HP, m.MaxHP, m.MapID, m.ReportedAt = old.Class, old.Level, old.HP, old.MaxHP, old.MapID, old.ReportedAt
			}
		}
		tx.PutMember(m)
		n.reachable(m) // first, so the party state below reaches the new connection
		if !refresh(tx, n, m) {
			n.alone(m.ID) // a party lost while away is not shown at login
		}
		return nil
	})
}

// Offline marks a character disconnected, party kept, when conn is still the connection they are online through.
func (s *PartyService) Offline(ctx context.Context, id kernel.CharacterID, conn string) error {
	return s.update(ctx, func(tx PartyTx, n *notes) error {
		m, ok := tx.Member(id)
		if !ok || m.Connection != conn {
			return nil
		}
		m.Online, m.OfflineAt = false, time.Now()
		tx.PutMember(m)
		n.unreachable(id, conn)
		refresh(tx, n, m)
		return nil
	})
}

// PruneMembers forgets at most limit members offline since before before and in no party, and reports how many.
func (s *PartyService) PruneMembers(ctx context.Context, before time.Time, limit int) (int64, error) {
	var n int64
	err := s.update(ctx, func(tx PartyTx, _ *notes) error {
		n = 0
		for _, m := range tx.Members() {
			if n == int64(limit) {
				break
			}
			if m.Online || !m.OfflineAt.Before(before) {
				continue
			}
			if _, ok := tx.PartyOf(m.ID); ok {
				continue // their party still lists them
			}
			tx.DeleteMember(m.ID)
			n++
		}
		return nil
	})
	return n, err
}

// reportKept is how long a world's report of a member social had not registered yet stands over their login's own, older, values.
const reportKept = time.Minute

// Update records a change the world saw: the map, level or HP of a member, kept for their login when social does not know them yet.
func (s *PartyService) Update(ctx context.Context, id kernel.CharacterID, mapID string, class kernel.Class, level, hp, maxHP int) error {
	return s.update(ctx, func(tx PartyTx, n *notes) error {
		now := time.Now()
		m, ok := tx.Member(id)
		if !ok {
			// Unknown yet, as after a restart: kept offline, with no connection, for the login to come.
			tx.PutMember(model.Member{ID: id, MapID: mapID, Class: class, Level: level, HP: hp, MaxHP: maxHP, OfflineAt: now, ReportedAt: now})
			return nil
		}
		m.MapID, m.Class, m.Level, m.HP, m.MaxHP, m.ReportedAt = mapID, class, level, hp, maxHP, now
		tx.PutMember(m)
		refresh(tx, n, m)
		return nil
	})
}

// Invite asks a character on the inviter's map to join; a partyless inviter leads once it is accepted.
func (s *PartyService) Invite(ctx context.Context, from, to kernel.CharacterID) (model.Invite, error) {
	var inv model.Invite
	err := s.update(ctx, func(tx PartyTx, n *notes) error {
		s.expire(tx)
		if from == to {
			return model.ErrSelf
		}
		inviter, ok := tx.Member(from)
		if !ok || !inviter.Online {
			return model.ErrNotHere // the world may have reported them before their login landed, without a name
		}
		if p, ok := tx.PartyOf(from); ok {
			if p.Leader != from {
				return model.ErrNotLeader
			}
			if len(p.Members) >= model.MaxMembers {
				return model.ErrFull
			}
		}
		target, ok := tx.Member(to)
		if !ok || !target.Online || target.MapID != inviter.MapID {
			return model.ErrNotHere
		}
		if _, ok := tx.PartyOf(to); ok {
			return model.ErrTargetInParty
		}
		if slices.ContainsFunc(tx.Invites(), func(i model.Invite) bool { return i.From == from && i.To == to }) {
			return model.ErrAlreadyAsked
		}
		inv = model.Invite{ID: model.NewInviteID(), From: from, FromName: inviter.Name, To: to, Expires: time.Now().Add(model.InviteTTL)}
		tx.PutInvite(inv)
		n.invited(to, inv)
		return nil
	})
	if err != nil {
		return model.Invite{}, err
	}
	return inv, nil
}

// Respond accepts or declines an invite addressed to who. The invite is spent even if joining is refused.
func (s *PartyService) Respond(ctx context.Context, who kernel.CharacterID, inviteID model.InviteID, accept bool) error {
	var refused error
	err := s.update(ctx, func(tx PartyTx, n *notes) error {
		s.expire(tx)
		inv, ok := tx.Invite(inviteID)
		if !ok || inv.To != who {
			return model.ErrInviteExpired
		}
		tx.DeleteInvite(inviteID)
		if accept {
			refused = s.join(tx, n, inv)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return refused
}

func (s *PartyService) join(tx PartyTx, n *notes, inv model.Invite) error {
	if _, ok := tx.PartyOf(inv.To); ok {
		return model.ErrInParty
	}
	member, ok := tx.Member(inv.To)
	if !ok {
		return model.ErrNotHere
	}
	p, ok := tx.PartyOf(inv.From)
	if ok && p.Leader != inv.From {
		return model.ErrInviteExpired // the inviter no longer leads the party it invited to
	}
	if !ok {
		leader, ok := tx.Member(inv.From)
		if !ok {
			return model.ErrInviteExpired
		}
		p = add(tx, model.Party{ID: model.NewPartyID(), Leader: inv.From}, leader)
	}
	if len(p.Members) >= model.MaxMembers {
		return model.ErrFull
	}
	p = add(tx, p, member)
	tx.PutParty(p)
	n.changed(p)
	return nil
}

// Leave removes who from their party; the longest-standing member replaces a leaving leader.
func (s *PartyService) Leave(ctx context.Context, who kernel.CharacterID) error {
	return s.update(ctx, func(tx PartyTx, n *notes) error {
		p, ok := tx.PartyOf(who)
		if !ok {
			return model.ErrNoParty
		}
		remove(tx, n, p, who)
		return nil
	})
}

// Kick removes a member; only the leader may, and not themselves.
func (s *PartyService) Kick(ctx context.Context, leader, target kernel.CharacterID) error {
	return s.update(ctx, func(tx PartyTx, n *notes) error {
		p, err := ledBy(tx, leader, target)
		if err != nil {
			return err
		}
		remove(tx, n, p, target)
		return nil
	})
}

// Promote hands leadership to another member.
func (s *PartyService) Promote(ctx context.Context, leader, target kernel.CharacterID) error {
	return s.update(ctx, func(tx PartyTx, n *notes) error {
		p, err := ledBy(tx, leader, target)
		if err != nil {
			return err
		}
		p.Leader = target
		tx.PutParty(p)
		n.changed(p)
		return nil
	})
}

// Of is the party of a character, or false.
func (s *PartyService) Of(ctx context.Context, id kernel.CharacterID) (model.Party, bool, error) {
	var p model.Party
	var ok bool
	err := s.store.InTx(ctx, func(tx PartyTx) error {
		p, ok = tx.PartyOf(id)
		return nil
	})
	return p, ok && err == nil, err
}

// Memberships visits every party's ID and member IDs; nothing changes until it returns, and visit must not block.
func (s *PartyService) Memberships(ctx context.Context, visit func(party model.PartyID, members []kernel.CharacterID)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var parties []model.Party
	if err := s.store.InTx(ctx, func(tx PartyTx) error {
		parties = tx.Parties()
		return nil
	}); err != nil {
		return err
	}
	for _, p := range parties {
		visit(p.ID, ids(p))
	}
	return nil
}

// Members lists the IDs of a character's party, nil without one.
func (s *PartyService) Members(ctx context.Context, id kernel.CharacterID) ([]kernel.CharacterID, error) {
	p, ok, err := s.Of(ctx, id)
	if err != nil || !ok {
		return nil, err
	}
	return ids(p), nil
}

// update runs fn as one transaction, then tells the notifier what fn queued.
func (s *PartyService) update(ctx context.Context, fn func(tx PartyTx, n *notes) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n notes
	if err := s.store.InTx(ctx, func(tx PartyTx) error {
		n = notes{}
		return fn(tx, &n)
	}); err != nil {
		return err
	}
	for _, send := range n {
		send(s.notifier)
	}
	return nil
}

func (s *PartyService) expire(tx PartyTx) {
	now := time.Now()
	for _, inv := range tx.Invites() {
		if !inv.Expires.After(now) {
			tx.DeleteInvite(inv.ID)
		}
	}
}

// ledBy is leader's party, if leader leads it and target is another member of it.
func ledBy(tx PartyTx, leader, target kernel.CharacterID) (model.Party, error) {
	p, ok := tx.PartyOf(leader)
	if !ok {
		return model.Party{}, model.ErrNoParty
	}
	if p.Leader != leader {
		return model.Party{}, model.ErrNotLeader
	}
	if tp, ok := tx.PartyOf(target); target == leader || !ok || tp.ID != p.ID {
		return model.Party{}, model.ErrNotMember
	}
	return p, nil
}

// refresh copies a member's row into their party and tells its members; false when they have none.
func refresh(tx PartyTx, n *notes, m model.Member) bool {
	p, ok := tx.PartyOf(m.ID)
	if !ok {
		return false
	}
	k := slices.IndexFunc(p.Members, func(x model.Member) bool { return x.ID == m.ID })
	if k < 0 {
		return false
	}
	m.JoinOrder = p.Members[k].JoinOrder
	p.Members[k] = m
	tx.PutParty(p)
	n.changed(p)
	return true
}

func add(tx PartyTx, p model.Party, m model.Member) model.Party {
	m.JoinOrder = tx.NextJoinOrder()
	p.Members = append(p.Members, m)
	return p
}

func remove(tx PartyTx, n *notes, p model.Party, id kernel.CharacterID) {
	p.Members = slices.DeleteFunc(p.Members, func(m model.Member) bool { return m.ID == id })
	n.left(id)
	if len(p.Members) == 0 {
		tx.DeleteParty(p.ID)
		return
	}
	if p.Leader == id {
		p.Leader = slices.MinFunc(p.Members, func(a, b model.Member) int { return int(a.JoinOrder) - int(b.JoinOrder) }).ID
	}
	tx.PutParty(p)
	n.changed(p)
}

func ids(p model.Party) []kernel.CharacterID {
	out := make([]kernel.CharacterID, 0, len(p.Members))
	for _, m := range p.Members {
		out = append(out, m.ID)
	}
	return out
}

// notes are the notifications of one change, sent once it commits.
type notes []func(Notifier)

func (n *notes) changed(p model.Party) {
	members := ids(p)
	state := &model.Party{ID: p.ID, Leader: p.Leader, Members: slices.Clone(p.Members)}
	*n = append(*n, func(to Notifier) { to.PartyChanged(members, state) })
}

func (n *notes) left(id kernel.CharacterID) {
	*n = append(*n, func(to Notifier) { to.PartyChanged([]kernel.CharacterID{id}, nil) })
}

func (n *notes) alone(id kernel.CharacterID) {
	*n = append(*n, func(to Notifier) { to.Alone(id) })
}

func (n *notes) invited(to kernel.CharacterID, inv model.Invite) {
	*n = append(*n, func(nt Notifier) { nt.Invited(to, inv) })
}

func (n *notes) reachable(m model.Member) {
	*n = append(*n, func(to Notifier) { to.Reachable(m.ID, m.Gateway, m.Connection) })
}

func (n *notes) unreachable(id kernel.CharacterID, conn string) {
	*n = append(*n, func(to Notifier) { to.Unreachable(id, conn) })
}
