package ws

import "finga/pkg/wsrouter"

// handler is one step of a message's chain: a middleware, or its end.
type handler = wsrouter.HandlerFunc[*client]

// msgContext is a message on its way through its chain.
type msgContext = wsrouter.Context[*client]

// limit is a per-message-type token bucket (page 4): a rate and a burst.
type limit struct {
	perSecond float64
	burst     int
}

// routes is the gateway's message table (page 4): every message a client
// may send, with the checks it passes on the read loop, the command queue
// that runs the rest of its chain in arrival order, and how it is
// answered. A new client message is one entry here and one handler in
// messages.go, as a REST endpoint is one route and one handler.
//
// Every message passes helloFirst. The read loop answers Ping and applies
// each command's rate limit, so a flood never fills the queue; queued
// hands the rest to the connection's command goroutine. World commands are
// fire and forget: the next input supersedes a failed one. Inventory and
// party commands are answered with a Response matched to their request ID
// (respond); a chat line is answered only when it is refused (refusals),
// since the line itself is the answer. ChatSend has no rate limit here:
// the social service limits it and answers chat_rate_limited, which a
// silent drop here would hide.
func (h *Handler) routes() *wsrouter.Router[*client] {
	r := wsrouter.New(h.helloFirst)
	// Market messages arrive in Stage 2; anything without a route is
	// refused in turn, behind the commands before it.
	r.NoRoute(h.queued, h.joined, h.notImplemented)

	wsrouter.Handle(r, "Hello", h.hello, h.queued)
	wsrouter.Handle(r, "Ping", h.ping)

	wsrouter.Handle(r, "Input", h.input, h.command(limit{30, 30})...) // one per tick is 20 a second; the headroom absorbs a burst after a stall
	wsrouter.Handle(r, "Attack", h.attack, h.command(limit{10, 10})...)
	wsrouter.Handle(r, "PickUp", h.pickUp, h.command(limit{10, 10})...)
	wsrouter.Handle(r, "EnterPortal", h.enterPortal, h.queued, h.joined, h.within)

	inventory := h.respond("inventory")
	wsrouter.Handle(r, "InventoryMove", h.inventoryMove, h.command(limit{5, 5}, inventory)...)
	wsrouter.Handle(r, "InventoryDiscard", h.inventoryDiscard, h.command(limit{5, 5}, inventory)...)

	party := h.respond("party")
	wsrouter.Handle(r, "PartyInvite", h.partyInvite, h.command(limit{2, 2}, party)...)
	wsrouter.Handle(r, "PartyRespond", h.partyRespond, h.command(limit{2, 2}, party)...)
	wsrouter.Handle(r, "PartyLeave", h.partyLeave, h.command(limit{2, 2}, party)...)
	wsrouter.Handle(r, "PartyKick", h.partyKick, h.command(limit{2, 2}, party)...)
	wsrouter.Handle(r, "PartyPromote", h.partyPromote, h.command(limit{2, 2}, party)...)

	wsrouter.Handle(r, "ChatSend", h.chatSend, h.queued, h.joined, h.within, h.refusals("ChatSend"))
	return r
}

// command is the chain of a rate-limited command: its limit on the read
// loop, then the queue, a joined player and a deadline, then rest.
func (h *Handler) command(l limit, rest ...handler) []handler {
	return append([]handler{h.limited(l), h.queued, h.joined, h.within}, rest...)
}
