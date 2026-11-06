package receive

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
		if !c.Session.allow(l, time.Now()) {
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

// traced runs the rest of the chain in a trace of the command's own, which EntryPoint decides to keep.
func (h *Handler) traced(c *msgContext) {
	ctx, span := h.tracer.Start(c.Ctx, spanPrefix+c.Message.Type, trace.WithNewRoot())
	defer span.End()
	c.Ctx = ctx
	c.Next()
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String("conn", c.Session.out.ID()))
	if id := c.Session.session.Character(); !id.IsZero() {
		span.SetAttributes(attribute.String("character_id", id.String()))
	}
	if c.Err != nil {
		span.SetStatus(codes.Error, c.Err.Error())
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

// logged ends a command that answers nothing; its unexpected failures are logged sampled, as inputs come 20 a second.
func (h *Handler) logged(c *msgContext) {
	c.Next()
	restarting := errors.Is(c.Err, model.ErrUnavailable) || errors.Is(c.Err, model.ErrNotSubscribed)
	if model.IsInternal(model.PublicOf(c.Err)) && !restarting && !closing(c) {
		h.failures.Do(func() { h.logFailure(c) })
	}
}

// closing reports whether the connection is closing, which cancels its commands: their failures are no fault.
func closing(c *msgContext) bool { return c.Session.ctx.Err() != nil }

// panicked answers internal and keeps the connection open, but closes it on a Hello, which nothing will answer now.
func (h *Handler) panicked(c *msgContext, e *panics.Error) {
	h.opts.Panics.Record(c.Ctx, panics.FlowWS, e, "conn", c.Session.out.ID(), "type", c.Message.Type, "req_id", c.Message.ReqID)
	if _, isHello := c.Message.Data.(*clientv1.Hello); isHello {
		c.Session.out.Close(wsconn.CloseInternal, "hello")
		return
	}
	replyError(c, model.Internal)
}

func (h *Handler) notImplemented(c *msgContext) {
	replyError(c, model.NotImplemented)
}

// publicError is what the client may see of the command's error; an unexpected one is logged with its cause.
func (h *Handler) publicError(c *msgContext) model.Public {
	p := model.PublicOf(c.Err)
	if model.IsInternal(p) && !closing(c) {
		h.logFailure(c)
	}
	return p
}

func (h *Handler) logFailure(c *msgContext) {
	h.opts.Logger.ErrorContext(c.Ctx, "command failed", "conn", c.Session.out.ID(), "character_id", c.Session.session.Character(), "type", c.Message.Type, "err", c.Err)
}

// replyError answers p to a message that has a request ID; without one nobody is waiting.
func replyError(c *msgContext, p model.Public) {
	if c.Message.ReqID != "" {
		c.Session.out.Reply(c.Message.ReqID, push.ErrorResponse(p))
	}
}
