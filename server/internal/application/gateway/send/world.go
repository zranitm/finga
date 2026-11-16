package send

import (
	"context"

	"finga/internal/application/gateway/session"
	model "finga/internal/model/gateway"
)

// Input hands the held keys to the player's map; world commands are fire and forget.
func (g *Gateway) Input(ctx context.Context, s *session.Session, seq uint32, keys model.Keys, view uint32) error {
	return g.deps.World.Input(ctx, s.Presence(), seq, keys, view)
}

// Attack swings or shoots the way the character faces, aimed along a yaw for the 3D world.
func (g *Gateway) Attack(ctx context.Context, s *session.Session, aim float32) error {
	return g.deps.World.Attack(ctx, s.Presence(), aim)
}

// EnterPortal takes the portal the player stands in.
func (g *Gateway) EnterPortal(ctx context.Context, s *session.Session) error {
	return g.deps.World.EnterPortal(ctx, s.Presence())
}
