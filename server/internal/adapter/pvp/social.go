package pvp

import (
	"context"

	"finga/internal/application/social"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// subscriber names the pvp module's subscription to party memberships, apart from every world's.
const subscriber = "pvp"

// SocialModule is the social module as the pvp module calls it.
type SocialModule interface {
	PartyOf(ctx context.Context, id kernel.CharacterID) (*social.Party, error)
	SubscribeMemberships(world string) *hub.Sub[social.MembershipEvent]
}

// Social adapts the social module to service.Social.
type Social struct {
	s      SocialModule
	panics *panics.Recorder
}

// NewSocial adapts s; rec records a panic handling the membership subscription.
func NewSocial(s SocialModule, rec *panics.Recorder) Social { return Social{s: s, panics: rec} }

// PartyOf is the character's party, false without one.
func (s Social) PartyOf(ctx context.Context, id kernel.CharacterID) (model.Party, bool, error) {
	p, err := s.s.PartyOf(ctx, id)
	if err != nil || p == nil {
		return model.Party{}, false, err
	}
	out := model.Party{Leader: p.Leader, Members: make([]model.Player, 0, len(p.Members))}
	for _, m := range p.Members {
		out.Members = append(out.Members, model.Player{ID: m.ID, Name: m.Name, Class: m.Class})
	}
	return out, true, nil
}

// FollowParties follows the memberships until ctx ends, subscribing again after a break; each subscription first resyncs, as changes in a break went unseen.
func (s Social) FollowParties(ctx context.Context, changed func(characters, members []kernel.CharacterID), resync func(ctx context.Context)) {
	hub.Follow(ctx, func() *hub.Sub[social.MembershipEvent] { return s.s.SubscribeMemberships(subscriber) }, func(ctx context.Context) error {
		resync(ctx)
		return nil
	}, func(e social.MembershipEvent) {
		if m, ok := e.(social.Membership); ok {
			changed(m.Characters, m.Members)
		}
	}, hub.FollowOptions{Panics: s.panics})
}
