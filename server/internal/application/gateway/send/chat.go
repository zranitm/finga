package send

import (
	"context"

	"finga/internal/application/gateway/session"
	model "finga/internal/model/gateway"
)

// Chat cleans a line via the social module, which delivers party lines itself; map lines go to the world.
func (g *Gateway) Chat(ctx context.Context, s *session.Session, channel model.ChatChannel, text string) error {
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
