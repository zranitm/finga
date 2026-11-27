package transport

import (
	"fmt"
	"math"
	"strings"

	"golang.org/x/time/rate"

	"finga/pkg/wsrouter"
)

// Step is one step of a message's chain: a middleware, or its end.
type Step = wsrouter.HandlerFunc[*Inbox]

// Context is a message on its way through its chain.
type Context = wsrouter.Context[*Inbox]

// Limit is a token bucket each connection keeps per group: the messages of a group share it.
type Limit struct {
	group     string
	perSecond float64
	burst     int
	perTick   float64 // when set, the rate and burst follow the world's tick rate: perTick a tick
}

// The limits, per connection.
var (
	LimitInput     = Limit{group: "input", perTick: 1.5} // one a tick, plus half again for a burst after a stall
	LimitAttack    = Limit{group: "attack", perSecond: 10, burst: 10}
	LimitSkill     = Limit{group: "skill", perSecond: 5, burst: 8} // eight bar keys at once; each skill's cooldown bounds it further
	LimitPortal    = Limit{group: "portal", perSecond: 2, burst: 2}
	LimitTeleport  = Limit{group: "teleport", perSecond: 2, burst: 2}  // teleports and channel switches together
	LimitChannels  = Limit{group: "channels", perSecond: 1, burst: 3}  // the map window asks every 5 s while open
	LimitBosses    = Limit{group: "bosses", perSecond: 1, burst: 3}    // so does a region's map window
	LimitInventory = Limit{group: "inventory", perSecond: 5, burst: 5} // moves, discards, equips and unequips together
	LimitPotion    = Limit{group: "potion", perSecond: 5, burst: 5}    // potions have no cooldown, so this is the only bound on drinking
	LimitParty     = Limit{group: "party", perSecond: 2, burst: 5}     // the five party commands together; a leader may use each at once
	LimitPoints    = Limit{group: "points", perSecond: 2, burst: 4}    // spends, resets and trait choices together: an Apply, or an auto-assign on a level-up
	LimitDialog    = Limit{group: "dialog", perSecond: 5, burst: 5}    // talks and the three quest commands together
	LimitPvp       = Limit{group: "pvp", perSecond: 2, burst: 4}       // queueing, leaving, answering and forfeiting together
	LimitLoot      = Limit{group: "loot", perSecond: 5, burst: 5}      // takes: a click on a row, or Take all
)

// assumedTickRate sizes a per-tick limit until a join reports the world's rate.
const assumedTickRate = 20

// at is the limit's rate and burst when the world ticks hz times a second.
func (l Limit) at(hz int) (rate.Limit, int) {
	if l.perTick == 0 {
		return rate.Limit(l.perSecond), l.burst
	}
	if hz <= 0 {
		hz = assumedTickRate
	}
	n := l.perTick * float64(hz)
	return rate.Limit(n), int(math.Ceil(n))
}

// spanPrefix starts the name of a command's span: ws.Hello.
const spanPrefix = "ws."

// sampled are the commands whose traces are kept: each answers after calls across modules; the rest only queue work.
var sampled = map[string]bool{
	"Hello": true, "InventoryMove": true, "InventoryDiscard": true, "InventoryDiscardMany": true, "Equip": true, "Unequip": true, "AddToWardrobe": true, "SetLook": true, "TakeLoot": true, "UseItem": true, "SpendPoints": true, "ResetPoints": true, "SpendSkillPoints": true, "ChooseTrait": true, "UseSkill": true, "Teleport": true, "SwitchChannel": true, "Channels": true, "Bosses": true, "Promote": true, "ChatSend": true,
	"PartyInvite": true, "PartyRespond": true, "PartyLeave": true, "PartyKick": true, "PartyPromote": true,
	"Talk": true, "AcceptQuest": true, "AbandonQuest": true, "HandInQuest": true,
	"PvpQueue": true, "PvpLeaveQueue": true, "PvpAnswer": true, "PvpForfeit": true,
}

// WSEntryPoint reports whether a root span of this name starts a trace that is kept.
func WSEntryPoint(span string) bool {
	command, ok := strings.CutPrefix(span, spanPrefix)
	return ok && sampled[command]
}

// Routes registers a module's client messages on a Receiver, each by Handle; a module's handler returns its own.
type Routes func(*Receiver)

// Handle routes the message type msgType to fn through chain; a second route for a type is recorded as an error, which NewReceiver returns.
func Handle[T any](h *Receiver, msgType string, fn func(*Context, T) (any, error), chain ...Step) {
	if h.names[msgType] {
		h.errs = append(h.errs, fmt.Errorf("transport: a second route for the message %s", msgType))
		return
	}
	h.names[msgType] = true
	wsrouter.Handle(h.router, msgType, fn, chain...)
}

// build makes the router: Hello first, then Ping, which the transport answers itself, then each module's routes.
func (h *Receiver) build(routes []Routes) {
	r := wsrouter.New(h.helloFirst)
	r.Recover(h.panicked)
	// Unrouted messages are refused in order, behind the commands queued before them.
	r.NoRoute(h.Queued, h.Joined, h.notImplemented)
	h.router = r
	Handle(h, "Ping", h.ping)
	for _, add := range routes {
		add(h)
	}
}

// Command is a rate-limited command's chain; the limit runs on the read loop so a flood never fills the queue.
func (h *Receiver) Command(l Limit, rest ...Step) []Step {
	return append([]Step{h.limited(l), h.Queued, h.Traced, h.Joined, h.Within}, rest...)
}

// Action is a rate-limited action's chain: a command's, on the action goroutine.
func (h *Receiver) Action(l Limit, rest ...Step) []Step {
	return append([]Step{h.limited(l), h.acted, h.Traced, h.Joined, h.Within}, rest...)
}
