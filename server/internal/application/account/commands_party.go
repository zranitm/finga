package account

import (
	"context"

	"finga/internal/shared/kernel"
)

// InviteToParty invites a character on the same map.
func (g *Commands) InviteToParty(ctx context.Context, s *Connection, to kernel.CharacterID) error {
	return g.deps.Social.Invite(ctx, s.Character(), to)
}

// AnswerInvite accepts or declines an invite.
func (g *Commands) AnswerInvite(ctx context.Context, s *Connection, inviteID string, accept bool) error {
	return g.deps.Social.Respond(ctx, s.Character(), inviteID, accept)
}

// LeaveParty leaves the player's party.
func (g *Commands) LeaveParty(ctx context.Context, s *Connection) error {
	return g.deps.Social.Leave(ctx, s.Character())
}

// KickFromParty removes a member; only the leader may.
func (g *Commands) KickFromParty(ctx context.Context, s *Connection, target kernel.CharacterID) error {
	return g.deps.Social.Kick(ctx, s.Character(), target)
}

// PromoteInParty hands the leadership to another member.
func (g *Commands) PromoteInParty(ctx context.Context, s *Connection, target kernel.CharacterID) error {
	return g.deps.Social.Promote(ctx, s.Character(), target)
}
