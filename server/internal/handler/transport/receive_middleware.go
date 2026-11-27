package transport

import (
	"context"
	"errors"
	"finga/internal/shared/failure"
	"finga/internal/shared/kernel"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/panics"
	"finga/pkg/transient"
	"finga/pkg/wsconn"
)

// helloFirst accepts only Hello first, and closes the connection on a second one.
func (h *Receiver) helloFirst(c *Context) {
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
func (h *Receiver) limited(l Limit) Step {
	return func(c *Context) {
		if !c.Session.allow(l, time.Now()) {
			replyError(c, failure.RateLimited)
			return
		}
		c.Next()
	}
}

// Queued hands the rest of the chain to the command goroutine; a full queue refuses with rate_limited.
func (h *Receiver) Queued(c *Context) { enqueue(c, c.Session.queue) }

// acted hands the rest of an action's chain to the action goroutine, apart from the commands; a full queue refuses with rate_limited.
func (h *Receiver) acted(c *Context) { enqueue(c, c.Session.actions) }

// enqueue puts the rest of c's chain on q, to run on the session's ctx; a full q refuses with rate_limited.
func enqueue(c *Context, q chan<- func()) {
	rest := c.Later()
	select {
	case q <- func() { c.Ctx = c.Session.ctx; rest() }:
	default:
		replyError(c, failure.RateLimited)
	}
}

// Traced runs the rest of the chain in a trace of the command's own, which WSEntryPoint decides to keep; the span is a server's, as the request a player sent.
func (h *Receiver) Traced(c *Context) {
	ctx, span := h.tracer.Start(c.Ctx, spanPrefix+c.Message.Type, trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindServer))
	c.Ctx = ctx
	panicked := true // until the chain returns: the router's handler then logs the panic under this trace's ID
	defer func() {
		if span.IsRecording() {
			annotate(span, c, panicked)
		}
		span.End()
	}()
	c.Next()
	panicked = false
}

func annotate(span trace.Span, c *Context, panicked bool) {
	span.SetAttributes(attribute.String("conn", c.Session.out.ID()))
	if id := c.Session.session.Character(); !id.IsZero() {
		span.SetAttributes(attribute.String("character_id", id.String()))
	}
	switch {
	case panicked:
		span.SetStatus(codes.Error, "panic")
	case c.Err != nil:
		span.SetStatus(codes.Error, c.Err.Error())
	}
}

// Joined drops commands from a connection whose Hello failed; it is closing.
func (h *Receiver) Joined(c *Context) {
	if !c.Session.joined {
		return
	}
	c.Next()
}

// Within gives the rest of the chain the command's time.
func (h *Receiver) Within(c *Context) {
	ctx, cancel := context.WithTimeout(c.Ctx, commandTimeout)
	defer cancel()
	c.Ctx = ctx
	c.Next()
}

// Respond answers a command with a Response, ok or its error, then with the handler's result if any.
func (h *Receiver) Respond(c *Context) {
	c.Next()
	if c.Err != nil {
		c.Session.out.Reply(c.Message.ReqID, RefusalResponse(h.publicError(c)))
		return
	}
	if r, ok := c.Result.(*clientv1.Response); ok { // a command whose answer is more than ok, as a TakeLoot's
		c.Session.out.Reply(c.Message.ReqID, r)
		return
	}
	c.Session.out.Reply(c.Message.ReqID, &clientv1.Response{Ok: true})
	if result, ok := c.Result.(proto.Message); ok {
		c.Session.out.Reply("", result)
	}
}

// RespondOnError answers a command only when it fails, request ID or not: ChatSend succeeds by its line coming back.
func (h *Receiver) RespondOnError(c *Context) {
	c.Next()
	if c.Err != nil {
		c.Session.out.Reply(c.Message.ReqID, RefusalResponse(h.publicError(c)))
	}
}

// Logged ends a command that answers nothing; its unexpected failures are logged sampled, as inputs come 20 a second.
func (h *Receiver) Logged(c *Context) {
	c.Next()
	restarting := errors.Is(c.Err, transient.ErrUnavailable) || errors.Is(c.Err, kernel.ErrNotSubscribed)
	if failure.IsInternal(failure.PublicOf(c.Err)) && !restarting && !closing(c) {
		h.failures.Do(func() { h.logFailure(c) })
	}
}

// closing reports whether the connection is closing, which cancels its commands: their failures are no fault.
func closing(c *Context) bool { return c.Session.ctx.Err() != nil }

// panicked answers internal and keeps the connection open, but closes it on a Hello, which nothing will answer now.
func (h *Receiver) panicked(c *Context, e *panics.Error) {
	h.opts.Panics.Record(c.Ctx, panics.FlowWS, e, "conn", c.Session.out.ID(), "type", c.Message.Type, "req_id", c.Message.ReqID)
	if _, isHello := c.Message.Data.(*clientv1.Hello); isHello {
		c.Session.out.Close(wsconn.CloseInternal, "hello")
		return
	}
	replyError(c, failure.Internal)
}

func (h *Receiver) notImplemented(c *Context) {
	replyError(c, failure.NotImplemented)
}

// publicError is what the client may see of the command's error; an unexpected one is logged with its cause.
func (h *Receiver) publicError(c *Context) failure.Public {
	p := failure.PublicOf(c.Err)
	if failure.IsInternal(p) && !closing(c) {
		h.logFailure(c)
	}
	return p
}

func (h *Receiver) logFailure(c *Context) {
	h.opts.Logger.ErrorContext(c.Ctx, "command failed", "conn", c.Session.out.ID(), "character_id", c.Session.session.Character(), "type", c.Message.Type, "err", c.Err)
}

// replyError answers p to a message that has a request ID; without one nobody is waiting.
func replyError(c *Context, p failure.Public) {
	if c.Message.ReqID != "" {
		c.Session.out.Reply(c.Message.ReqID, RefusalResponse(p))
	}
}
