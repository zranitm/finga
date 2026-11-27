package pvp

import (
	"context"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/pvp"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"
)

// Queue is the pvp module as the client's messages call it.
type Queue interface {
	Queue(ctx context.Context, p pvp.Player, mode string) error
	LeaveQueue(ctx context.Context, who kernel.CharacterID) error
	Answer(ctx context.Context, who kernel.CharacterID, offer string, accept bool) error
}

// ws is the module's client messages: client to server. Forfeit is the world's, which holds the matches.
type ws struct {
	pvp Queue
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(q Queue) transport.Routes {
	h := &ws{pvp: q}
	return func(r *transport.Receiver) {
		transport.Handle(r, "PvpQueue", h.pvpQueue, r.Command(transport.LimitPvp, r.Respond)...)
		transport.Handle(r, "PvpLeaveQueue", h.pvpLeaveQueue, r.Command(transport.LimitPvp, r.Respond)...)
		transport.Handle(r, "PvpAnswer", h.pvpAnswer, r.Command(transport.LimitPvp, r.Respond)...)
	}
}

func (h *ws) pvpQueue(c *transport.Context, in *clientv1.PvpQueue) (any, error) {
	who := c.Session.Session().Player()
	return nil, h.pvp.Queue(c.Ctx, pvp.Player{ID: who.ID, Name: who.Name, Class: kernel.Class(who.Class)}, in.GetMode())
}

func (h *ws) pvpLeaveQueue(c *transport.Context, _ *clientv1.PvpLeaveQueue) (any, error) {
	return nil, h.pvp.LeaveQueue(c.Ctx, c.Session.Session().Character())
}

func (h *ws) pvpAnswer(c *transport.Context, in *clientv1.PvpAnswer) (any, error) {
	return nil, h.pvp.Answer(c.Ctx, c.Session.Session().Character(), in.GetOffer(), in.GetAccept())
}
