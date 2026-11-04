package send

import (
	"context"

	clientv1 "finga/api/gen/client/v1"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// Chat cleans a line via the social service, which delivers party lines itself; map lines go to the world.
func (g *Gateway) Chat(ctx context.Context, s *session.Session, channel clientv1.ChatChannel, text string) error {
	if g.deps.Social == nil {
		return model.ErrNoChat
	}
	sender := s.Player()
	line, err := g.deps.Social.Chat(ctx, sender, channel, text)
	if err != nil {
		return err
	}
	if line.Channel == clientv1.ChatChannel_CHAT_CHANNEL_MAP {
		_ = g.deps.World.Say(ctx, s.Presence(), sender.Name, line.Text) // fire and forget, as every world command
	}
	return nil
}
