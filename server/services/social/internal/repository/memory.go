// Package repository keeps the social service's party state.
package repository

import (
	"context"
	"maps"
	"slices"
	"sync"

	"finga/internal/kernel"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
)

// Memory is a PartyStore in process memory: one process owns the state, and a restart loses it.
type Memory struct {
	mu       sync.Mutex
	members  map[kernel.CharacterID]model.Member
	parties  map[string]model.Party
	byMember map[kernel.CharacterID]string
	invites  map[string]model.Invite
	seq      uint64
}

var _ application.PartyStore = (*Memory)(nil)

// NewMemory makes an empty store.
func NewMemory() *Memory {
	return &Memory{
		members:  map[kernel.CharacterID]model.Member{},
		parties:  map[string]model.Party{},
		byMember: map[kernel.CharacterID]string{},
		invites:  map[string]model.Invite{},
	}
}

// InTx runs fn on staged writes and applies them only if fn returns nil.
func (s *Memory) InTx(_ context.Context, fn func(application.PartyTx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := &memoryTx{s: s, members: map[kernel.CharacterID]model.Member{}, parties: map[string]*model.Party{}, invites: map[string]*model.Invite{}, seq: s.seq}
	if err := fn(tx); err != nil {
		return err
	}
	tx.commit()
	return nil
}

// memoryTx stages writes over the store; a nil party or invite is a deletion.
type memoryTx struct {
	s       *Memory
	members map[kernel.CharacterID]model.Member
	parties map[string]*model.Party
	invites map[string]*model.Invite
	seq     uint64
}

// Member is a character seen online.
func (t *memoryTx) Member(id kernel.CharacterID) (model.Member, bool) {
	if m, ok := t.members[id]; ok {
		return m, true
	}
	m, ok := t.s.members[id]
	return m, ok
}

// PutMember saves a character's row.
func (t *memoryTx) PutMember(m model.Member) { t.members[m.ID] = m }

// PartyOf finds a party by any of its members.
func (t *memoryTx) PartyOf(member kernel.CharacterID) (model.Party, bool) {
	for _, p := range t.parties {
		if p != nil && slices.ContainsFunc(p.Members, func(m model.Member) bool { return m.ID == member }) {
			return clone(*p), true
		}
	}
	id, ok := t.s.byMember[member]
	if !ok {
		return model.Party{}, false
	}
	if _, staged := t.parties[id]; staged {
		return model.Party{}, false // rewritten without them, or deleted
	}
	return clone(t.s.parties[id]), true
}

// PutParty saves a party.
func (t *memoryTx) PutParty(p model.Party) {
	p = clone(p)
	t.parties[p.ID] = &p
}

// DeleteParty removes a party.
func (t *memoryTx) DeleteParty(id string) { t.parties[id] = nil }

// Parties lists every party.
func (t *memoryTx) Parties() []model.Party {
	var out []model.Party
	for id, p := range t.s.parties {
		if _, staged := t.parties[id]; !staged {
			out = append(out, clone(p))
		}
	}
	for _, p := range t.parties {
		if p != nil {
			out = append(out, clone(*p))
		}
	}
	return out
}

// Invite is a pending invite.
func (t *memoryTx) Invite(id string) (model.Invite, bool) {
	if inv, staged := t.invites[id]; staged {
		if inv == nil {
			return model.Invite{}, false
		}
		return *inv, true
	}
	inv, ok := t.s.invites[id]
	return inv, ok
}

// PutInvite saves an invite.
func (t *memoryTx) PutInvite(inv model.Invite) { t.invites[inv.ID] = &inv }

// DeleteInvite removes an invite.
func (t *memoryTx) DeleteInvite(id string) { t.invites[id] = nil }

// Invites lists the pending invites.
func (t *memoryTx) Invites() []model.Invite {
	var out []model.Invite
	for id, inv := range t.s.invites {
		if _, staged := t.invites[id]; !staged {
			out = append(out, inv)
		}
	}
	for _, inv := range t.invites {
		if inv != nil {
			out = append(out, *inv)
		}
	}
	return out
}

// NextJoinOrder hands out the next join order.
func (t *memoryTx) NextJoinOrder() uint64 {
	t.seq++
	return t.seq
}

func (t *memoryTx) commit() {
	s := t.s
	maps.Copy(s.members, t.members)
	for id, p := range t.parties {
		for _, m := range s.parties[id].Members {
			if s.byMember[m.ID] == id {
				delete(s.byMember, m.ID)
			}
		}
		if p == nil {
			delete(s.parties, id)
			continue
		}
		s.parties[id] = *p
		for _, m := range p.Members {
			s.byMember[m.ID] = id
		}
	}
	for id, inv := range t.invites {
		if inv == nil {
			delete(s.invites, id)
		} else {
			s.invites[id] = *inv
		}
	}
	s.seq = t.seq
}

func clone(p model.Party) model.Party {
	p.Members = slices.Clone(p.Members)
	return p
}
