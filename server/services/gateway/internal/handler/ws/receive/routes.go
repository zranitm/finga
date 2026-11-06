package receive

import (
	"strings"

	"finga/pkg/wsrouter"
)

// handler is one step of a message's chain: a middleware, or its end.
type handler = wsrouter.HandlerFunc[*Inbox]

type msgContext = wsrouter.Context[*Inbox]

// limit is a token bucket each connection keeps per group: the messages of a group share it.
type limit struct {
	group     string
	perSecond float64
	burst     int
}

// The limits, per connection.
var (
	inputs    = limit{"input", 30, 30} // 20 ticks a second, plus headroom for a burst after a stall
	attacks   = limit{"attack", 10, 10}
	pickups   = limit{"pickup", 10, 10}
	portals   = limit{"portal", 2, 2}
	inventory = limit{"inventory", 5, 5} // moves and discards together
	party     = limit{"party", 2, 5}     // the five party commands together; a leader may use each at once
)

// spanPrefix starts the name of a command's span: ws.Hello.
const spanPrefix = "ws."

// sampled are the commands whose traces are kept: each answers after calls across services. The others only
// queue work for a tick, so their traces are dropped, though their calls and log lines still carry the ID.
var sampled = map[string]bool{
	"Hello": true, "InventoryMove": true, "InventoryDiscard": true, "ChatSend": true,
	"PartyInvite": true, "PartyRespond": true, "PartyLeave": true, "PartyKick": true, "PartyPromote": true,
}

// EntryPoint reports whether a root span of this name starts a trace that is kept.
func EntryPoint(span string) bool {
	command, ok := strings.CutPrefix(span, spanPrefix)
	return ok && sampled[command]
}

// routes maps every client message to its middleware chain and handler.
func (h *Handler) routes() *wsrouter.Router[*Inbox] {
	r := wsrouter.New(h.helloFirst)
	r.Recover(h.panicked)
	// Unrouted messages are refused in order, behind the commands queued before them.
	r.NoRoute(h.queued, h.joined, h.notImplemented)

	wsrouter.Handle(r, "Hello", h.hello, h.queued, h.traced)
	wsrouter.Handle(r, "Ping", h.ping)

	wsrouter.Handle(r, "Input", h.input, h.command(inputs, h.logged)...)
	wsrouter.Handle(r, "Attack", h.attack, h.command(attacks, h.logged)...)
	wsrouter.Handle(r, "PickUp", h.pickUp, h.command(pickups, h.logged)...)
	wsrouter.Handle(r, "EnterPortal", h.enterPortal, h.command(portals, h.logged)...)

	wsrouter.Handle(r, "InventoryMove", h.inventoryMove, h.command(inventory, h.respond)...)
	wsrouter.Handle(r, "InventoryDiscard", h.inventoryDiscard, h.command(inventory, h.respond)...)

	wsrouter.Handle(r, "PartyInvite", h.partyInvite, h.command(party, h.respond)...)
	wsrouter.Handle(r, "PartyRespond", h.partyRespond, h.command(party, h.respond)...)
	wsrouter.Handle(r, "PartyLeave", h.partyLeave, h.command(party, h.respond)...)
	wsrouter.Handle(r, "PartyKick", h.partyKick, h.command(party, h.respond)...)
	wsrouter.Handle(r, "PartyPromote", h.partyPromote, h.command(party, h.respond)...)

	// No limit here: the social service answers chat_rate_limited itself.
	wsrouter.Handle(r, "ChatSend", h.chatSend, h.queued, h.traced, h.joined, h.within, h.respondOnError)
	return r
}

// command is a rate-limited command's chain; the limit runs on the read loop so a flood never fills the queue.
func (h *Handler) command(l limit, rest ...handler) []handler {
	return append([]handler{h.limited(l), h.queued, h.traced, h.joined, h.within}, rest...)
}
