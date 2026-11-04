package send

import (
	"context"

	"finga/internal/kernel"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// InviteToParty invites a character on the same map.
func (g *Gateway) InviteToParty(ctx context.Context, s *session.Session, to kernel.CharacterID) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Invite(ctx, s.Character(), to)
}

// AnswerInvite accepts or declines an invite.
func (g *Gateway) AnswerInvite(ctx context.Context, s *session.Session, inviteID string, accept bool) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Respond(ctx, s.Character(), inviteID, accept)
}

// LeaveParty leaves the player's party.
func (g *Gateway) LeaveParty(ctx context.Context, s *session.Session) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Leave(ctx, s.Character())
}

// KickFromParty removes a member; only the leader may.
func (g *Gateway) KickFromParty(ctx context.Context, s *session.Session, target kernel.CharacterID) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Kick(ctx, s.Character(), target)
}

// PromoteInParty hands the leadership to another member.
func (g *Gateway) PromoteInParty(ctx context.Context, s *session.Session, target kernel.CharacterID) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Promote(ctx, s.Character(), target)
}
