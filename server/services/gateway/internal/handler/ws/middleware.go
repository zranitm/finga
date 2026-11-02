package ws

import (
	"context"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/model"
)

// helloFirst accepts only Hello first, and closes the connection on a second one.
func (h *Handler) helloFirst(c *msgContext) {
	cl := c.Session
	_, isHello := c.Message.Data.(*clientv1.Hello)
	switch {
	case !cl.helloSeen && !isHello:
		cl.conn.Close(wsconn.CloseKicked, "hello expected")
		return
	case !cl.helloSeen:
		cl.helloSeen = true
		cl.helloTimer.Stop()
	case isHello:
		cl.conn.Close(wsconn.CloseKicked, "hello twice")
		return
	}
	c.Next()
}

// limited charges the message type's bucket; over the limit, the message is refused or dropped.
func (h *Handler) limited(l limit) handler {
	return func(c *msgContext) {
		if !c.Session.allow(c.Message.Type, l) {
			refuseWith(c, model.RateLimited)
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
		refuseWith(c, model.RateLimited)
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

// respond answers with a Response matched to the request ID, then the handler's result if any.
func (h *Handler) respond(label string) handler {
	return func(c *msgContext) {
		c.Next()
		cl := c.Session
		if c.Err != nil {
			cl.reply(c.Message.ReqID, h.refuse(c, label))
			return
		}
		cl.reply(c.Message.ReqID, &clientv1.Response{Ok: true})
		if result, ok := c.Result.(proto.Message); ok {
			cl.reply("", result)
		}
	}
}

func (h *Handler) refusals(label string) handler {
	return func(c *msgContext) {
		c.Next()
		if c.Err != nil {
			c.Session.reply(c.Message.ReqID, h.refuse(c, label))
		}
	}
}

// panicked answers a message whose chain panicked with internal; the connection stays open.
func (h *Handler) panicked(c *msgContext, e *panics.Error) {
	h.opts.Panics.Record(c.Ctx, panics.FlowWS, e, "conn", c.Session.ID(), "type", c.Message.Type, "req_id", c.Message.ReqID)
	refuseWith(c, model.Internal)
}

func (h *Handler) notImplemented(c *msgContext) {
	refuseWith(c, model.NotImplemented)
}

// refuse turns a command's error into a Response, logging unexpected ones.
func (h *Handler) refuse(c *msgContext, command string) *clientv1.Response {
	p := model.PublicOf(c.Err)
	if model.IsInternal(p) {
		h.opts.Logger.Error("command failed", "conn", c.Session.ID(), "character_id", c.Session.session.Character(), "command", command, "err", c.Err)
	}
	return refusal(p)
}

// refuseWith answers p to a message with a request ID; without one nobody is waiting.
func refuseWith(c *msgContext, p model.Public) {
	if c.Message.ReqID != "" {
		c.Session.reply(c.Message.ReqID, refusal(p))
	}
}

func refusal(p model.Public) *clientv1.Response {
	return &clientv1.Response{Ok: false, Code: p.Code, Message: p.Message}
}
