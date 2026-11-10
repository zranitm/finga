package send

import (
	"context"

	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// Input hands the held keys to the player's map; world commands are fire and forget.
func (g *Gateway) Input(ctx context.Context, s *session.Session, seq uint32, keys model.Keys) error {
	return g.deps.World.Input(ctx, s.Presence(), seq, keys)
}

// Attack swings or shoots the way the character faces.
func (g *Gateway) Attack(ctx context.Context, s *session.Session) error {
	return g.deps.World.Attack(ctx, s.Presence())
}

// EnterPortal takes the portal the player stands in.
func (g *Gateway) EnterPortal(ctx context.Context, s *session.Session) error {
	return g.deps.World.EnterPortal(ctx, s.Presence())
}
