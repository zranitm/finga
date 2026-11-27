package pvp

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/gateway/send"
	"finga/internal/handler/transport"
)

// ws is the module's client messages: client to server.
type ws struct {
	gw *send.Gateway
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(gw *send.Gateway) transport.Routes {
	h := &ws{gw: gw}
	return func(r *transport.Receiver) {
		transport.Handle(r, "PvpQueue", h.pvpQueue, r.Command(transport.LimitPvp, r.Respond)...)
		transport.Handle(r, "PvpLeaveQueue", h.pvpLeaveQueue, r.Command(transport.LimitPvp, r.Respond)...)
		transport.Handle(r, "PvpAnswer", h.pvpAnswer, r.Command(transport.LimitPvp, r.Respond)...)
		transport.Handle(r, "PvpForfeit", h.pvpForfeit, r.Command(transport.LimitPvp, r.Respond)...)
	}
}

func (h *ws) pvpQueue(c *transport.Context, in *clientv1.PvpQueue) (any, error) {
	return nil, h.gw.QueuePvp(c.Ctx, c.Session.Session(), in.GetMode())
}

func (h *ws) pvpLeaveQueue(c *transport.Context, _ *clientv1.PvpLeaveQueue) (any, error) {
	return nil, h.gw.LeavePvpQueue(c.Ctx, c.Session.Session())
}

func (h *ws) pvpAnswer(c *transport.Context, in *clientv1.PvpAnswer) (any, error) {
	return nil, h.gw.AnswerPvp(c.Ctx, c.Session.Session(), in.GetOffer(), in.GetAccept())
}

func (h *ws) pvpForfeit(c *transport.Context, _ *clientv1.PvpForfeit) (any, error) {
	return nil, h.gw.ForfeitMatch(c.Ctx, c.Session.Session())
}
