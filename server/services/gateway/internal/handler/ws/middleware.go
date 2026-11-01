package ws

import (
	"context"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/model"
)

// helloFirst is the handshake rule: before Hello only Hello is accepted,
// and a second Hello ends the connection. The first Hello stops the
// connection's Hello timer.
func (h *Handler) helloFirst(c *msgContext) {
	cl := c.Session
	_, isHello := c.Message.Data.(protocol.Hello)
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

// limited charges the message's type from the connection's bucket for it
// (page 4); over the limit, a command with a request ID is answered
// rate_limited and the rest dropped.
func (h *Handler) limited(l limit) handler {
	return func(c *msgContext) {
		if !c.Session.allow(c.Message.Type, l) {
			refuseWith(c, model.RateLimited)
			return
		}
		c.Next()
	}
}

// queued hands the rest of the chain to the connection's command
// goroutine, which runs it in order with the connection's context. A full
// queue answers rate_limited to a command with a request ID and drops the
// rest.
func (h *Handler) queued(c *msgContext) {
	select {
	case c.Session.queue <- func() { c.Ctx = c.Session.ctx; c.Next() }:
	default:
		refuseWith(c, model.RateLimited)
	}
}

// joined drops a command whose connection has no player: its Hello failed,
// and the connection is closing.
func (h *Handler) joined(c *msgContext) {
	if !c.Session.joined {
		return
	}
	c.Next()
}

// within gives the rest of the chain a deadline, ended early when the
// connection closes.
func (h *Handler) within(c *msgContext) {
	ctx, cancel := context.WithTimeout(c.Ctx, commandTimeout)
	defer cancel()
	c.Ctx = ctx
	c.Next()
}

// respond answers a command with a Response matched to its request ID: OK,
// then what the handler returned if anything (InventoryChanged), or the
// refusal its error names.
func (h *Handler) respond(label string) handler {
	return func(c *msgContext) {
		c.Next()
		cl := c.Session
		if c.Err != nil {
			cl.reply(c.Message.ReqID, h.refuse(c, label))
			return
		}
		cl.reply(c.Message.ReqID, protocol.Response{OK: true})
		if c.Result != nil {
			cl.reply("", c.Result)
		}
	}
}

// refusals answers a command only when it is refused.
func (h *Handler) refusals(label string) handler {
	return func(c *msgContext) {
		c.Next()
		if c.Err != nil {
			c.Session.reply(c.Message.ReqID, h.refuse(c, label))
		}
	}
}

// notImplemented refuses a message the gateway has no route for yet.
func (h *Handler) notImplemented(c *msgContext) {
	refuseWith(c, model.NotImplemented)
}

// refuse is a command's error as a Response, with its own code and
// message; the cause of an error nobody expected (a database failure),
// which the player only sees as internal, is logged.
func (h *Handler) refuse(c *msgContext, command string) protocol.Response {
	p := model.PublicOf(c.Err)
	if model.IsInternal(p) {
		h.opts.Logger.Error("command failed", "conn", c.Session.ID(), "character_id", c.Session.session.Character(), "command", command, "err", c.Err)
	}
	return refusal(p)
}

// refuseWith answers a command with a request ID with p; one without a
// request ID has nobody waiting for an answer.
func refuseWith(c *msgContext, p model.Public) {
	if c.Message.ReqID != "" {
		c.Session.reply(c.Message.ReqID, refusal(p))
	}
}

// refusal is a failure as a Response.
func refusal(p model.Public) protocol.Response {
	return protocol.Response{OK: false, Code: p.Code, Message: p.Message}
}
