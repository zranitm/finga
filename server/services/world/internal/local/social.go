package local

import (
	"context"

	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/social"
	"finga/services/world/internal/model"
)

// SocialModule is the social module as the world calls it.
type SocialModule interface {
	SubscribeMemberships(world string) *hub.Sub[social.MembershipEvent]
	Memberships(ctx context.Context, visit func(party social.PartyID, members []kernel.CharacterID)) error
	UpdateMember(ctx context.Context, u social.MemberUpdate) error
	Announce(ctx context.Context, a social.Announcement) error
}

// Social adapts the social module to party.Social and announce.Social.
type Social struct {
	s      SocialModule
	panics *panics.Recorder
}

// NewSocial adapts the social module; rec records a panic handling the party subscription.
func NewSocial(s SocialModule, rec *panics.Recorder) Social { return Social{s: s, panics: rec} }

// FollowParties follows the memberships as world until ctx ends, subscribing again after a break: each subscription tells PartiesSubscribed, then the whole table, then each change since.
func (s Social) FollowParties(ctx context.Context, world string, on func(model.PartyEvent)) {
	hub.Follow(ctx, func(ctx context.Context) (*hub.Sub[social.MembershipEvent], error) {
		sub := s.s.SubscribeMemberships(world)
		on(model.PartiesSubscribed{})
		if err := s.s.Memberships(ctx, func(party social.PartyID, members []kernel.CharacterID) {
			on(membershipOf(social.Membership{Characters: members, Members: members, Party: party}))
		}); err != nil {
			sub.Close()
			return nil, err
		}
		return sub, nil
	}, func(e social.MembershipEvent) {
		if m, ok := e.(social.Membership); ok {
			on(membershipOf(m))
		}
	}, hub.FollowOptions{Panics: s.panics, OnBreak: func(err error) { on(model.PartiesLost{Err: err}) }})
}

// membershipOf is a membership as the party copy keeps it, in slices of its own: every world hears the same one.
func membershipOf(m social.Membership) model.Membership {
	party := model.Party{Members: ids(m.Members)}
	if !m.Party.IsZero() {
		party.ID = m.Party.String()
	}
	return model.Membership{Characters: ids(m.Characters), Party: party}
}

func ids(in []kernel.CharacterID) []kernel.CharacterID {
	return append(make([]kernel.CharacterID, 0, len(in)), in...)
}

// UpdateMember tells the social module what a map saw of a member.
func (s Social) UpdateMember(ctx context.Context, u model.MemberUpdate) error {
	return s.s.UpdateMember(ctx, social.MemberUpdate{ID: u.ID, MapID: u.MapID, Class: u.Class, Level: u.Level, HP: u.HP, MaxHP: u.MaxHP})
}

// Announce sends news to every character online.
func (s Social) Announce(ctx context.Context, a model.Announcement) error {
	switch a := a.(type) { //nolint:gocritic // a switch, so gochecksumtype names every kind added later
	case model.BossAppeared:
		return s.s.Announce(ctx, social.BossAppeared{Boss: a.Boss, MapID: a.Map})
	}
	return nil
}

// Retryable is a failure the social module may yet get past, as a transient code is over gRPC; any other would be retried forever.
func (Social) Retryable(err error) bool { return transientErr(err) }
