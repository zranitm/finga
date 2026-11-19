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

// Attack swings or shoots along a yaw, at the lock first when its shape reaches it.
func (g *Gateway) Attack(ctx context.Context, s *session.Session, aim float32, target model.Target) error {
	return g.deps.World.Attack(ctx, s.Presence(), aim, target)
}

// EnterPortal takes the portal the player stands in.
func (g *Gateway) EnterPortal(ctx context.Context, s *session.Session) error {
	return g.deps.World.EnterPortal(ctx, s.Presence())
}

// Teleport starts a teleport channel to the map through the world, which answers once it starts; the MapChange or TeleportCancelled comes on its stream.
func (g *Gateway) Teleport(ctx context.Context, s *session.Session, mapID string, seq uint32) error {
	return g.deps.World.Teleport(ctx, s.Presence(), mapID, seq)
}
