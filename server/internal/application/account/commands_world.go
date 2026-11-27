package account

import (
	"context"

	model "finga/internal/model/account"
)

// Input hands the held keys to the player's map; world commands are fire and forget.
func (g *Commands) Input(ctx context.Context, s *Connection, seq uint32, keys model.Keys, view uint32) error {
	return g.deps.World.Input(ctx, s.Presence(), seq, keys, view)
}

// Attack swings or shoots along a yaw, at the lock first when its shape reaches it.
func (g *Commands) Attack(ctx context.Context, s *Connection, aim float32, target model.Target) error {
	return g.deps.World.Attack(ctx, s.Presence(), aim, target)
}

// EnterPortal takes the portal the player stands in.
func (g *Commands) EnterPortal(ctx context.Context, s *Connection) error {
	return g.deps.World.EnterPortal(ctx, s.Presence())
}

// Teleport starts a teleport channel to the waypoint, or else the old map, through the world, which answers once it starts; the MapChange or TeleportCancelled comes on its stream.
func (g *Commands) Teleport(ctx context.Context, s *Connection, mapID, waypoint string, seq uint32) error {
	return g.deps.World.Teleport(ctx, s.Presence(), mapID, waypoint, seq)
}

// SwitchChannel starts a switch to channel n of the player's region through the world, which answers once it starts; the MapChange or TeleportCancelled comes on its stream.
func (g *Commands) SwitchChannel(ctx context.Context, s *Connection, n int, seq uint32) error {
	return g.deps.World.SwitchChannel(ctx, s.Presence(), n, seq)
}

// Channels are the open channels of the player's region and how full each is.
func (g *Commands) Channels(ctx context.Context, s *Connection) (model.Channels, error) {
	return g.deps.World.Channels(ctx, s.Presence())
}

// Bosses is how each hunting area's field boss stands on the player's region channel.
func (g *Commands) Bosses(ctx context.Context, s *Connection) ([]model.AreaBoss, error) {
	return g.deps.World.Bosses(ctx, s.Presence())
}
