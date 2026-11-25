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

// Teleport starts a teleport channel to the waypoint, or else the old map, through the world, which answers once it starts; the MapChange or TeleportCancelled comes on its stream.
func (g *Gateway) Teleport(ctx context.Context, s *session.Session, mapID, waypoint string, seq uint32) error {
	return g.deps.World.Teleport(ctx, s.Presence(), mapID, waypoint, seq)
}

// SwitchChannel starts a switch to channel n of the player's region through the world, which answers once it starts; the MapChange or TeleportCancelled comes on its stream.
func (g *Gateway) SwitchChannel(ctx context.Context, s *session.Session, n int, seq uint32) error {
	return g.deps.World.SwitchChannel(ctx, s.Presence(), n, seq)
}

// Channels are the open channels of the player's region and how full each is.
func (g *Gateway) Channels(ctx context.Context, s *session.Session) (model.Channels, error) {
	return g.deps.World.Channels(ctx, s.Presence())
}

// Bosses is how each hunting area's field boss stands on the player's region channel.
func (g *Gateway) Bosses(ctx context.Context, s *session.Session) ([]model.AreaBoss, error) {
	return g.deps.World.Bosses(ctx, s.Presence())
}
