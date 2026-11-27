package account

import (
	"context"

	model "finga/internal/model/account"
)

// Chat cleans a line via the social module, which delivers party lines itself; map lines go to the world.
func (g *Commands) Chat(ctx context.Context, s *Connection, channel model.ChatChannel, text string) error {
	sender := s.Player()
	line, err := g.deps.Social.Chat(ctx, sender, channel, text)
	if err != nil {
		return err
	}
	if line.Channel == model.ChatMap {
		_ = g.deps.World.Say(ctx, s.Presence(), sender.Name, line.Text) // fire and forget, as every world command
	}
	return nil
}
