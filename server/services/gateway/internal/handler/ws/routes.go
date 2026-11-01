package ws

import "finga/pkg/wsrouter"

// handler is one step of a message's chain: a middleware, or its end.
type handler = wsrouter.HandlerFunc[*client]

type msgContext = wsrouter.Context[*client]

// limit is a per-message-type token bucket: a rate and a burst.
type limit struct {
	perSecond float64
	burst     int
}

// routes maps every client message to its middleware chain and handler.
func (h *Handler) routes() *wsrouter.Router[*client] {
	r := wsrouter.New(h.helloFirst)
	// Unrouted messages are refused in order, behind the commands queued before them.
	r.NoRoute(h.queued, h.joined, h.notImplemented)

	wsrouter.Handle(r, "Hello", h.hello, h.queued)
	wsrouter.Handle(r, "Ping", h.ping)

	wsrouter.Handle(r, "Input", h.input, h.command(limit{30, 30})...) // 20 ticks a second, plus headroom for a burst after a stall
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

	// No limit here: the social service answers chat_rate_limited itself.
	wsrouter.Handle(r, "ChatSend", h.chatSend, h.queued, h.joined, h.within, h.refusals("ChatSend"))
	return r
}

// command is a rate-limited command's chain; the limit runs on the read loop so a flood never fills the queue.
func (h *Handler) command(l limit, rest ...handler) []handler {
	return append([]handler{h.limited(l), h.queued, h.joined, h.within}, rest...)
}
