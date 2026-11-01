package application

import (
	"context"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

// Input hands the held keys to the player's map; world commands are fire and forget.
func (g *Gateway) Input(ctx context.Context, s *Session, seq uint32, keys model.Keys) error {
	return g.deps.World.Input(ctx, s.presence(), seq, keys)
}

// Attack swings or shoots the way the character faces.
func (g *Gateway) Attack(ctx context.Context, s *Session) error {
	return g.deps.World.Attack(ctx, s.presence())
}

// PickUp picks up the drop under the player; the outcome arrives on the world's stream.
func (g *Gateway) PickUp(ctx context.Context, s *Session) error {
	return g.deps.World.PickUp(ctx, s.presence())
}

// EnterPortal takes the portal the player stands in.
func (g *Gateway) EnterPortal(ctx context.Context, s *Session) error {
	return g.deps.World.EnterPortal(ctx, s.presence())
}

// MoveItem moves an item and returns the slots it changed.
func (g *Gateway) MoveItem(ctx context.Context, s *Session, in model.ItemMove) ([]*clientv1.InventorySlot, error) {
	if g.deps.Items == nil {
		return nil, model.ErrNoInventory
	}
	return g.deps.Items.Move(ctx, s.actor(), in)
}

// DiscardItem destroys some or all of a stack and returns the slots it changed.
func (g *Gateway) DiscardItem(ctx context.Context, s *Session, in model.ItemDiscard) ([]*clientv1.InventorySlot, error) {
	if g.deps.Items == nil {
		return nil, model.ErrNoInventory
	}
	return g.deps.Items.Discard(ctx, s.actor(), in)
}

// InviteToParty invites a character on the same map.
func (g *Gateway) InviteToParty(ctx context.Context, s *Session, to kernel.CharacterID) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Invite(ctx, s.character.ID, to)
}

// AnswerInvite accepts or declines an invite.
func (g *Gateway) AnswerInvite(ctx context.Context, s *Session, inviteID string, accept bool) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Respond(ctx, s.character.ID, inviteID, accept)
}

// LeaveParty leaves the player's party.
func (g *Gateway) LeaveParty(ctx context.Context, s *Session) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Leave(ctx, s.character.ID)
}

// KickFromParty removes a member; only the leader may.
func (g *Gateway) KickFromParty(ctx context.Context, s *Session, target kernel.CharacterID) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Kick(ctx, s.character.ID, target)
}

// PromoteInParty hands the leadership to another member.
func (g *Gateway) PromoteInParty(ctx context.Context, s *Session, target kernel.CharacterID) error {
	if g.deps.Social == nil {
		return model.ErrNoParties
	}
	return g.deps.Social.Promote(ctx, s.character.ID, target)
}

// Chat cleans a line via the social service, which delivers party lines itself; map lines go to the world.
func (g *Gateway) Chat(ctx context.Context, s *Session, channel clientv1.ChatChannel, text string) error {
	if g.deps.Social == nil {
		return model.ErrNoChat
	}
	line, err := g.deps.Social.Chat(ctx, s.character, channel, text)
	if err != nil {
		return err
	}
	if line.Channel == clientv1.ChatChannel_CHAT_CHANNEL_MAP {
		_ = g.deps.World.Say(ctx, s.presence(), s.character.Name, line.Text) // fire and forget, as every world command
	}
	return nil
}
