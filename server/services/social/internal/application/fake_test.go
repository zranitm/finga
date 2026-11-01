package application_test

import (
	"context"
	"slices"

	"finga/internal/kernel"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
)

// fakeStore is a PartyStore on plain maps: writes land at once, and fail makes every transaction fail.
type fakeStore struct {
	fail    error
	members map[kernel.CharacterID]model.Member
	parties map[model.PartyID]model.Party
	invites map[model.InviteID]model.Invite
	seq     uint64
}

func newFakeStore() *fakeStore {
	return &fakeStore{members: map[kernel.CharacterID]model.Member{}, parties: map[model.PartyID]model.Party{}, invites: map[model.InviteID]model.Invite{}}
}

func (s *fakeStore) InTx(_ context.Context, fn func(application.PartyTx) error) error {
	if s.fail != nil {
		return s.fail
	}
	return fn(s)
}

func (s *fakeStore) Member(id kernel.CharacterID) (model.Member, bool) {
	m, ok := s.members[id]
	return m, ok
}

func (s *fakeStore) PutMember(m model.Member) { s.members[m.ID] = m }

func (s *fakeStore) PartyOf(id kernel.CharacterID) (model.Party, bool) {
	for _, p := range s.parties {
		if slices.ContainsFunc(p.Members, func(m model.Member) bool { return m.ID == id }) {
			return clone(p), true
		}
	}
	return model.Party{}, false
}

func (s *fakeStore) PutParty(p model.Party)       { s.parties[p.ID] = clone(p) }
func (s *fakeStore) DeleteParty(id model.PartyID) { delete(s.parties, id) }

func (s *fakeStore) Parties() []model.Party {
	out := make([]model.Party, 0, len(s.parties))
	for _, p := range s.parties {
		out = append(out, clone(p))
	}
	return out
}

func (s *fakeStore) Invite(id model.InviteID) (model.Invite, bool) {
	inv, ok := s.invites[id]
	return inv, ok
}

func (s *fakeStore) PutInvite(inv model.Invite)     { s.invites[inv.ID] = inv }
func (s *fakeStore) DeleteInvite(id model.InviteID) { delete(s.invites, id) }

func (s *fakeStore) Invites() []model.Invite {
	out := make([]model.Invite, 0, len(s.invites))
	for _, inv := range s.invites {
		out = append(out, inv)
	}
	return out
}

func (s *fakeStore) NextJoinOrder() uint64 {
	s.seq++
	return s.seq
}

func clone(p model.Party) model.Party {
	p.Members = slices.Clone(p.Members)
	return p
}
