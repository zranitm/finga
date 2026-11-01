package application

import (
	"context"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/wsrouter"
	"finga/services/gateway/internal/model"
)

// handler is one step of a message's chain: a middleware, or its end.
type handler = wsrouter.HandlerFunc[*session]

// msgContext is a message on its way through its chain.
type msgContext = wsrouter.Context[*session]

// routes is the gateway's message table (page 4): every message a client
// may send, with the checks it passes on the read loop, the command queue
// that runs the rest of its chain in arrival order, and how it is
// answered. A new client message is one entry here.
//
// Every message passes helloFirst. The read loop answers Ping and applies
// each command's rate limit, so a flood never fills the queue; queued
// hands the rest to the connection's command goroutine. World commands are
// fire and forget: the next input supersedes a failed one,
// and a world that went away is the rejoin's to handle. Inventory and
// party commands are answered with a Response matched to their request ID
// (respond); a chat line is answered only when it is refused (refusals),
// since the line itself is the answer. ChatSend has no rate limit here:
// the social service limits it and answers chat_rate_limited, which a
// silent drop here would hide.
func (g *Gateway) routes() *wsrouter.Router[*session] {
	r := wsrouter.New(g.helloFirst)
	// Market messages arrive in Stage 2; anything without a route is
	// refused in turn, behind the commands before it.
	r.NoRoute(g.queued, g.joined, g.notImplemented)

	wsrouter.Handle(r, "Hello", g.helloMessage, g.queued)
	wsrouter.Handle(r, "Ping", g.ping)

	wsrouter.Handle(r, "Input", g.input, g.command(30, 30)...) // one per tick is 20 a second; the headroom absorbs a burst after a stall
	wsrouter.Handle(r, "Attack", g.attack, g.command(10, 10)...)
	wsrouter.Handle(r, "PickUp", g.pickUp, g.command(10, 10)...)
	wsrouter.Handle(r, "EnterPortal", g.enterPortal, g.queued, g.joined, g.within)

	inventory := g.answered(g.needs(g.deps.Items != nil, model.InventoryUnavailable), "inventory")
	wsrouter.Handle(r, "InventoryMove", g.inventoryMove, g.command(5, 5, inventory...)...)
	wsrouter.Handle(r, "InventoryDiscard", g.inventoryDiscard, g.command(5, 5, inventory...)...)

	party := g.answered(g.needs(g.deps.Social != nil, model.PartiesUnavailable), "party")
	wsrouter.Handle(r, "PartyInvite", g.partyInvite, g.command(2, 2, party...)...)
	wsrouter.Handle(r, "PartyRespond", g.partyRespond, g.command(2, 2, party...)...)
	wsrouter.Handle(r, "PartyLeave", g.partyLeave, g.command(2, 2, party...)...)
	wsrouter.Handle(r, "PartyKick", g.partyKick, g.command(2, 2, party...)...)
	wsrouter.Handle(r, "PartyPromote", g.partyPromote, g.command(2, 2, party...)...)

	wsrouter.Handle(r, "ChatSend", g.chatSend, g.queued, g.joined, g.needs(g.deps.Social != nil, model.ChatUnavailable), g.within, g.refusals("ChatSend"))
	return r
}

// command is the chain of a rate-limited command: its limit on the read
// loop, then the queue, a joined player and a deadline, then rest.
func (g *Gateway) command(perSecond float64, burst int, rest ...handler) []handler {
	return append([]handler{g.limited(model.Limit{PerSecond: perSecond, Burst: burst}), g.queued, g.joined, g.within}, rest...)
}

// answered is the end of a command a service answers: the service must be
// there, and the client gets a Response.
func (g *Gateway) answered(needs handler, label string) []handler {
	return []handler{needs, g.respond(label)}
}

// --- Middleware ---

// helloFirst is the handshake rule: before Hello only Hello is accepted,
// and a second Hello ends the connection.
func (g *Gateway) helloFirst(c *msgContext) {
	s := c.Session
	_, isHello := c.Message.Data.(protocol.Hello)
	switch {
	case !s.helloSeen && !isHello:
		s.conn.Close(model.CloseKicked, "hello expected")
		return
	case !s.helloSeen:
		s.helloSeen = true
	case isHello:
		s.conn.Close(model.CloseKicked, "hello twice")
		return
	}
	c.Next()
}

// limited charges the message's type from the connection's bucket for it
// (page 4); over the limit, a command with a request ID is answered
// rate_limited and the rest dropped.
func (g *Gateway) limited(lim model.Limit) handler {
	return func(c *msgContext) {
		if !c.Session.allow(c.Message.Type, lim, g.now) {
			g.refuseWith(c, model.RateLimited)
			return
		}
		c.Next()
	}
}

// queued hands the rest of the chain to the connection's command
// goroutine, which runs it in arrival order. A full queue answers
// rate_limited to a command with a request ID and drops the rest.
func (g *Gateway) queued(c *msgContext) {
	select {
	case c.Session.queue <- c.Next:
	default:
		g.refuseWith(c, model.RateLimited)
	}
}

// joined drops a command whose connection has no player: its Hello failed,
// and the connection is closing.
func (g *Gateway) joined(c *msgContext) {
	if !c.Session.active() {
		return
	}
	c.Next()
}

// needs refuses a command whose service this gateway runs without. The
// refusal always goes out, with the request ID when there is one: a chat
// line carries none, and its sender still learns chat is unavailable.
func (g *Gateway) needs(available bool, missing model.Public) handler {
	return func(c *msgContext) {
		if !available {
			g.send(c.Session.conn, model.Message{ReqID: c.Message.ReqID, Data: refusal(missing)})
			return
		}
		c.Next()
	}
}

// within gives the rest of the chain a deadline, ended early when the
// connection closes.
func (g *Gateway) within(c *msgContext) {
	ctx, cancel := context.WithTimeout(c.Session.ctx, commandTimeout)
	defer cancel()
	c.Ctx = ctx
	c.Next()
}

// respond answers a command with a Response matched to its request ID: OK,
// then what the handler returned if anything (InventoryChanged), or the
// refusal its error names.
func (g *Gateway) respond(label string) handler {
	return func(c *msgContext) {
		c.Next()
		s := c.Session
		if c.Err != nil {
			g.send(s.conn, model.Message{ReqID: c.Message.ReqID, Data: g.refuse(s, label, c.Err)})
			return
		}
		g.send(s.conn, model.Message{ReqID: c.Message.ReqID, Data: protocol.Response{OK: true}})
		if c.Result != nil {
			g.send(s.conn, model.Message{Data: c.Result})
		}
	}
}

// refusals answers a command only when it is refused.
func (g *Gateway) refusals(label string) handler {
	return func(c *msgContext) {
		c.Next()
		if c.Err != nil {
			g.send(c.Session.conn, model.Message{ReqID: c.Message.ReqID, Data: g.refuse(c.Session, label, c.Err)})
		}
	}
}

// notImplemented refuses a message the gateway has no route for yet.
func (g *Gateway) notImplemented(c *msgContext) {
	g.refuseWith(c, model.NotImplemented)
}

// refuseWith answers a command with a request ID with p; one without a
// request ID has nobody waiting for an answer.
func (g *Gateway) refuseWith(c *msgContext, p model.Public) {
	if c.Message.ReqID != "" {
		g.send(c.Session.conn, model.Message{ReqID: c.Message.ReqID, Data: refusal(p)})
	}
}

// --- Handlers ---

func (g *Gateway) helloMessage(c *msgContext, h protocol.Hello) (any, error) {
	g.hello(c.Session, h)
	return nil, nil
}

func (g *Gateway) ping(c *msgContext, p protocol.Ping) (any, error) {
	g.send(c.Session.conn, model.Message{Data: protocol.Pong{ClientTime: p.ClientTime, ServerTime: g.now().UnixMilli()}})
	return nil, nil
}

func (g *Gateway) input(c *msgContext, in protocol.Input) (any, error) {
	return nil, g.deps.World.Input(c.Ctx, c.Session.presence(), c.Message.Seq, in)
}

func (g *Gateway) attack(c *msgContext, _ protocol.Attack) (any, error) {
	return nil, g.deps.World.Attack(c.Ctx, c.Session.presence())
}

func (g *Gateway) pickUp(c *msgContext, _ protocol.PickUp) (any, error) {
	return nil, g.deps.World.PickUp(c.Ctx, c.Session.presence())
}

func (g *Gateway) enterPortal(c *msgContext, _ protocol.EnterPortal) (any, error) {
	return nil, g.deps.World.EnterPortal(c.Ctx, c.Session.presence())
}

func (g *Gateway) inventoryMove(c *msgContext, in protocol.InventoryMove) (any, error) {
	id, err := kernel.ParseItemID(in.ItemID)
	if err != nil {
		return nil, err
	}
	in.ItemID = id.String()
	return changed(g.deps.Items.Move(c.Ctx, c.Session.actor(), in))
}

func (g *Gateway) inventoryDiscard(c *msgContext, in protocol.InventoryDiscard) (any, error) {
	id, err := kernel.ParseItemID(in.ItemID)
	if err != nil {
		return nil, err
	}
	in.ItemID = id.String()
	return changed(g.deps.Items.Discard(c.Ctx, c.Session.actor(), in))
}

// changed is an inventory command's follow-up for the client: the slots
// it changed, when there are any.
func changed(slots []protocol.InventorySlot, err error) (any, error) {
	if err != nil || len(slots) == 0 {
		return nil, err
	}
	return protocol.InventoryChanged{Slots: slots}, nil
}

func (g *Gateway) partyInvite(c *msgContext, in protocol.PartyInvite) (any, error) {
	to, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		return nil, err
	}
	return nil, g.deps.Social.Invite(c.Ctx, c.Session.character.ID, to)
}

func (g *Gateway) partyRespond(c *msgContext, in protocol.PartyRespond) (any, error) {
	return nil, g.deps.Social.Respond(c.Ctx, c.Session.character.ID, in.InviteID, in.Accept)
}

func (g *Gateway) partyLeave(c *msgContext, _ protocol.PartyLeave) (any, error) {
	return nil, g.deps.Social.Leave(c.Ctx, c.Session.character.ID)
}

func (g *Gateway) partyKick(c *msgContext, in protocol.PartyKick) (any, error) {
	target, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		return nil, err
	}
	return nil, g.deps.Social.Kick(c.Ctx, c.Session.character.ID, target)
}

func (g *Gateway) partyPromote(c *msgContext, in protocol.PartyPromote) (any, error) {
	target, err := kernel.ParseCharacterID(in.CharacterID)
	if err != nil {
		return nil, err
	}
	return nil, g.deps.Social.Promote(c.Ctx, c.Session.character.ID, target)
}

// chatSend cleans a line through the social service, then hands a map
// line to the sender's map instance (everyone there hears it, the sender
// too); the social service delivers a party line to each member itself, on
// any map and through any gateway.
func (g *Gateway) chatSend(c *msgContext, in protocol.ChatSend) (any, error) {
	s := c.Session
	line, err := g.deps.Social.Chat(c.Ctx, s.character, in.Channel, in.Text)
	if err != nil {
		return nil, err
	}
	if line.Channel == protocol.ChatMap {
		_ = g.deps.World.Say(c.Ctx, s.presence(), s.character.Name, line.Text) // fire and forget, as every world command
	}
	return nil, nil
}
