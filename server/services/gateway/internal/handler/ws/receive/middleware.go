package receive

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/handler/ws/push"
	"finga/services/gateway/internal/model"
)

// helloFirst accepts only Hello first, and closes the connection on a second one.
func (h *Handler) helloFirst(c *msgContext) {
	inbox := c.Session
	_, isHello := c.Message.Data.(*clientv1.Hello)
	switch {
	case !inbox.helloSeen && !isHello:
		inbox.out.Close(wsconn.CloseKicked, "hello expected")
		return
	case !inbox.helloSeen:
		inbox.helloSeen = true
		inbox.helloTimer.Stop()
	case isHello:
		inbox.out.Close(wsconn.CloseKicked, "hello twice")
		return
	}
	c.Next()
}

// limited charges the message type's bucket; over the limit, the message is refused or dropped.
func (h *Handler) limited(l limit) handler {
	return func(c *msgContext) {
		if !c.Session.allow(c.Message.Type, l, time.Now()) {
			replyError(c, model.RateLimited)
			return
		}
		c.Next()
	}
}

// queued hands the rest of the chain to the command goroutine; a full queue refuses with rate_limited.
func (h *Handler) queued(c *msgContext) {
	rest := c.Later()
	select {
	case c.Session.queue <- func() { c.Ctx = c.Session.ctx; rest() }:
	default:
		replyError(c, model.RateLimited)
	}
}

// joined drops commands from a connection whose Hello failed; it is closing.
func (h *Handler) joined(c *msgContext) {
	if !c.Session.joined {
		return
	}
	c.Next()
}

func (h *Handler) within(c *msgContext) {
	ctx, cancel := context.WithTimeout(c.Ctx, commandTimeout)
	defer cancel()
	c.Ctx = ctx
	c.Next()
}

// respond answers a command with a Response, ok or its error, then with the handler's result if any.
func (h *Handler) respond(c *msgContext) {
	c.Next()
	if c.Err != nil {
		c.Session.out.Reply(c.Message.ReqID, push.ErrorResponse(h.publicError(c)))
		return
	}
	c.Session.out.Reply(c.Message.ReqID, &clientv1.Response{Ok: true})
	if result, ok := c.Result.(proto.Message); ok {
		c.Session.out.Reply("", result)
	}
}

// respondOnError answers a command only when it fails, request ID or not: ChatSend succeeds by its line coming back.
func (h *Handler) respondOnError(c *msgContext) {
	c.Next()
	if c.Err != nil {
		c.Session.out.Reply(c.Message.ReqID, push.ErrorResponse(h.publicError(c)))
	}
}

// panicked answers a message whose chain panicked with internal; the connection stays open.
func (h *Handler) panicked(c *msgContext, e *panics.Error) {
	h.opts.Panics.Record(c.Ctx, panics.FlowWS, e, "conn", c.Session.out.ID(), "type", c.Message.Type, "req_id", c.Message.ReqID)
	replyError(c, model.Internal)
}

func (h *Handler) notImplemented(c *msgContext) {
	replyError(c, model.NotImplemented)
}

// publicError is what the client may see of the command's error; an unexpected one is logged with its cause.
func (h *Handler) publicError(c *msgContext) model.Public {
	p := model.PublicOf(c.Err)
	if model.IsInternal(p) {
		h.opts.Logger.Error("command failed", "conn", c.Session.out.ID(), "character_id", c.Session.session.Character(), "type", c.Message.Type, "err", c.Err)
	}
	return p
}

// replyError answers p to a message that has a request ID; without one nobody is waiting.
func replyError(c *msgContext, p model.Public) {
	if c.Message.ReqID != "" {
		c.Session.out.Reply(c.Message.ReqID, push.ErrorResponse(p))
	}
}
