package account

import (
	"errors"
	application "finga/internal/application/account"
	"finga/internal/shared/failure"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
)

// ws is the module's client messages: client to server.
type ws struct {
	gw *application.Commands
}

// WSRoutes adds the module's client messages to a Receiver.
func WSRoutes(gw *application.Commands) transport.Routes {
	h := &ws{gw: gw}
	return func(r *transport.Receiver) {
		transport.Handle(r, "Hello", h.hello, r.Queued, r.Traced)
	}
}

// hello checks the protocol version and character ID, then joins; a rejected Hello is kicked.
func (h *ws) hello(c *transport.Context, in *clientv1.Hello) (any, error) {
	inbox := c.Session
	joined := false
	defer func() { inbox.HelloAnswered(joined) }() // the actions wait for it
	if in.GetProtocolVersion() != protocol.Version {
		inbox.Out().Kick(clientv1.KickReason_KICK_REASON_OUTDATED_PROTOCOL)
		return nil, nil
	}
	id, err := kernel.ParseCharacterID(in.GetCharacterId())
	if err != nil {
		inbox.Out().Kick(clientv1.KickReason_KICK_REASON_UNAUTHENTICATED)
		return nil, nil
	}
	switch err := h.gw.Hello(c.Ctx, inbox.Session(), in.GetToken(), id); {
	case err == nil:
		joined = true
	case errors.Is(err, failure.ErrHelloRejected):
		inbox.Out().Kick(clientv1.KickReason_KICK_REASON_UNAUTHENTICATED)
	default:
		inbox.Out().Close(wsconn.CloseInternal, "hello")
	}
	return nil, nil
}
