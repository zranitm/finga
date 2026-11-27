package account

import (
	"context"
)

// QueuePvp puts the player in a mode's queue.
func (g *Commands) QueuePvp(ctx context.Context, s *Connection, mode string) error {
	return g.deps.Pvp.Queue(ctx, s.Player(), mode)
}

// LeavePvpQueue takes the player out of the queue.
func (g *Commands) LeavePvpQueue(ctx context.Context, s *Connection) error {
	return g.deps.Pvp.LeaveQueue(ctx, s.Character())
}

// AnswerPvp accepts or declines an offer.
func (g *Commands) AnswerPvp(ctx context.Context, s *Connection, offer string, accept bool) error {
	return g.deps.Pvp.Answer(ctx, s.Character(), offer, accept)
}

// ForfeitMatch takes the player out of their match as a loss; during the outcome it only takes them back.
func (g *Commands) ForfeitMatch(ctx context.Context, s *Connection) error {
	return g.deps.World.Forfeit(ctx, s.Presence())
}
