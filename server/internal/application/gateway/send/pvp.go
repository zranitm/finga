package send

import (
	"context"

	"finga/internal/application/gateway/session"
)

// QueuePvp puts the player in a mode's queue.
func (g *Gateway) QueuePvp(ctx context.Context, s *session.Session, mode string) error {
	return g.deps.Pvp.Queue(ctx, s.Player(), mode)
}

// LeavePvpQueue takes the player out of the queue.
func (g *Gateway) LeavePvpQueue(ctx context.Context, s *session.Session) error {
	return g.deps.Pvp.LeaveQueue(ctx, s.Character())
}

// AnswerPvp accepts or declines an offer.
func (g *Gateway) AnswerPvp(ctx context.Context, s *session.Session, offer string, accept bool) error {
	return g.deps.Pvp.Answer(ctx, s.Character(), offer, accept)
}

// ForfeitMatch takes the player out of their match as a loss; during the outcome it only takes them back.
func (g *Gateway) ForfeitMatch(ctx context.Context, s *session.Session) error {
	return g.deps.World.Forfeit(ctx, s.Presence())
}
