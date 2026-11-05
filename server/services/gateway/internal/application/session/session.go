// Package session holds what the gateway's send and push flows share: each connection's Session,
// the Registry that finds a player's, and the joins that put a player on its map.
package session

import (
	"context"
	"sync"
	"sync/atomic"

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

	// who the session plays: set whole by a join, never changed in place, so reading it takes no lock
	id     atomic.Pointer[identity]
	closed bool // under the registry's lock: a rejoin finishing after Remove leaves again

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
func (s *Session) Deliver(d model.Delivery) {
	if !s.out.deliver(d) {
		s.outbox.End(model.EndDeliveryBacklog)
	}
}

// DeliverNow hands a delivery straight to the player, past the join's hold.
func (s *Session) DeliverNow(d model.Delivery) { s.outbox.Deliver(d) }

// End ends the player's session; the first End wins.
func (s *Session) End(reason model.EndReason) { s.outbox.End(reason) }

// out holds deliveries until a join's Welcome or MapChange is out, and shifts ticks so they never go
// back after a world restart. The world routes by connection, so every delivery is this session's.
type out struct {
	mu       sync.Mutex
	mapID    string
	holding  bool
	pending  []model.Delivery
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

// deliver emits or holds a delivery; false means too much is waiting.
func (o *out) deliver(d model.Delivery) bool {
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

// emit forwards a delivery with its tick shifted. Called with mu held.
func (o *out) emit(d model.Delivery) {
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
