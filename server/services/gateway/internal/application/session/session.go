// Package session holds what the send and push flows share: each connection's Session, the Registry and the joins.
package session

import (
	"context"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

// pendingMax bounds deliveries held during a join; more means something is wrong.
const pendingMax = 128

// Session is one connection's state from Open to Remove; its first join sets its identity.
type Session struct {
	connID string
	outbox Outbox
	// ctx ends at Remove, so a rejoin in flight stops.
	ctx      context.Context
	cancel   context.CancelFunc
	isActive atomic.Bool
	tickRate atomic.Int32  // the world's ticks a second, from the latest join; 0 before one
	joinSeq  atomic.Uint64 // the world's sequence of the latest join, which social orders logins by
	rejoin   atomic.Bool   // a rejoin is waiting to start, so another asked for meanwhile is the same one

	// who the session plays: set whole by a join, never changed in place, so reading it takes no lock
	id     atomic.Pointer[identity]
	closed bool // under the registry's lock: a rejoin finishing after Remove leaves again

	social *semaphore.Weighted // one call at a time tells the social service of the player: a registration or the close's Offline

	out out
}

// identity is who a session plays; a join replaces it whole.
type identity struct {
	account   kernel.AccountID
	sessionID kernel.SessionID
	character model.Player
}

// who is the session's identity; zero until a join begins.
func (s *Session) who() identity {
	if p := s.id.Load(); p != nil {
		return *p
	}
	return identity{}
}

// TickRateHz is the world's ticks a second as the latest join reported them, 0 before a join.
func (s *Session) TickRateHz() int { return int(s.tickRate.Load()) }

// ConnID names the session's connection.
func (s *Session) ConnID() string { return s.connID }

// Character is the character the session plays; zero until a join claims one.
func (s *Session) Character() kernel.CharacterID { return s.who().character.ID }

// Player is the character as its party sees it, once a join finished.
func (s *Session) Player() model.Player { return s.who().character }

// LoginSession is the login session the player's token belongs to.
func (s *Session) LoginSession() kernel.SessionID { return s.who().sessionID }

// Active reports whether a join finished: the player is on its map.
func (s *Session) Active() bool { return s.isActive.Load() }

func (s *Session) setActive() { s.isActive.Store(true) }

// Deliver hands a world delivery to the player through the join's hold; too many held ends the session.
func (s *Session) Deliver(d model.Delivery) { s.deliver(d, true) }

// DeliverSocial hands a social delivery to the player through the join's hold; it has no world tick to shift.
func (s *Session) DeliverSocial(d model.Delivery) { s.deliver(d, false) }

func (s *Session) deliver(d model.Delivery, world bool) {
	if !s.out.deliver(held{d, world}) {
		s.outbox.End(model.EndDeliveryBacklog)
	}
}

// End ends the player's session; the first End wins.
func (s *Session) End(reason model.EndReason) { s.outbox.End(reason) }

// out holds deliveries until a join's Welcome or MapChange is out, and shifts ticks so they never go back on a restart.
type out struct {
	mu       sync.Mutex
	mapID    string
	holding  bool
	pending  []held
	lastTick uint64
	offset   uint64
	rebase   bool // the next Snapshot may start the ticks again
	outbox   Outbox
}

func (o *out) hold() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.pending, o.rebase = true, nil, true
}

// held is a delivery waiting for the join; a world one has its tick shifted when it goes out.
type held struct {
	d     model.Delivery
	world bool
}

// deliver emits or holds a delivery; false means too much is waiting.
func (o *out) deliver(d held) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.holding {
		o.emit(d)
		return true
	}
	if len(o.pending) >= pendingMax {
		return false
	}
	o.pending = append(o.pending, d)
	return true
}

// release sends first (if set), then the held deliveries in order.
func (o *out) release(mapID string, first func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.mapID = mapID
	if first != nil {
		first()
	}
	for _, d := range o.pending {
		o.emit(d)
	}
	o.holding, o.pending = false, nil
}

// abort ends a join that failed: nothing it held is coming.
func (o *out) abort() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.pending, o.rebase = false, nil, false
}

// emit forwards a delivery, a world one with its tick shifted. Called with mu held.
func (o *out) emit(h held) {
	d := h.d
	if !h.world {
		o.outbox.Deliver(d)
		return
	}
	if _, ok := d.Push.(model.Snapshot); ok && o.rebase {
		if d.Tick+o.offset <= o.lastTick {
			o.offset = o.lastTick + 1 - d.Tick
		}
		o.rebase = false
	}
	d.Tick += o.offset
	o.lastTick = max(o.lastTick, d.Tick)
	if mc, ok := d.Push.(model.MapChange); ok {
		o.mapID = mc.MapID
	}
	o.outbox.Deliver(d)
}

// Presence is the player and its connection; every world command carries it.
func (s *Session) Presence() model.Presence {
	return model.Presence{Character: s.Character(), Connection: s.connID}
}

// Actor is who acts on items.
func (s *Session) Actor() model.Actor {
	who := s.who()
	return model.Actor{Account: who.account, Character: who.character.ID}
}

func (s *Session) mapID() string {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return s.out.mapID
}
