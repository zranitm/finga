package application

import (
	"context"
	"sync"
	"sync/atomic"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

// pendingMax bounds deliveries held during a join; more means something is wrong.
const pendingMax = 128

// Session is one connection's state from Open to Closed; Hello sets its identity once.
type Session struct {
	connID string
	outbox Outbox
	// ctx ends at Closed, so a rejoin in flight stops.
	ctx      context.Context
	cancel   context.CancelFunc
	isActive atomic.Bool

	// Set by Hello under the gateway's lock, before the session is active.
	account   kernel.AccountID
	sessionID kernel.SessionID
	character model.Player
	closed    bool // a rejoin finishing after Closed leaves again

	rejoinMu sync.Mutex // one join at a time

	out out
}

// Character is the character the session plays; zero until Hello joins.
func (s *Session) Character() kernel.CharacterID { return s.character.ID }

func (s *Session) active() bool { return s.isActive.Load() }
func (s *Session) setActive()   { s.isActive.Store(true) }

// out holds deliveries until a join's Welcome or MapChange is out, drops those before its Snapshot
// (an older presence's), and shifts ticks so they never go back after a world restart.
type out struct {
	mu       sync.Mutex
	mapID    string
	holding  bool
	awaiting bool // the join's Snapshot has not arrived yet
	pending  []model.Delivery
	lastTick uint64
	offset   uint64
	rebase   bool // the next Snapshot may start the ticks again
	outbox   Outbox
}

func (o *out) hold() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.awaiting, o.pending, o.rebase = true, true, nil, true
}

// deliver emits, holds or drops a delivery; false means too much is waiting.
func (o *out) deliver(d model.Delivery) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.awaiting {
		if d.Type != model.DeliverySnapshot {
			return true
		}
		o.awaiting = false
	}
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

// abort ends a join that failed: nothing it held, or awaited, is coming.
func (o *out) abort() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.awaiting, o.pending, o.rebase = false, false, nil, false
}

// emit forwards a delivery with its tick shifted. Called with mu held.
func (o *out) emit(d model.Delivery) {
	if o.rebase && d.Type == model.DeliverySnapshot {
		if d.Tick+o.offset <= o.lastTick {
			o.offset = o.lastTick + 1 - d.Tick
		}
		o.rebase = false
	}
	d.Tick += o.offset
	o.lastTick = max(o.lastTick, d.Tick)
	if d.MapID != "" {
		o.mapID = d.MapID
	}
	o.outbox.Deliver(d)
}

func (s *Session) presence() model.Presence {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return model.Presence{Character: s.character.ID, MapID: s.out.mapID}
}

func (s *Session) actor() model.Actor {
	return model.Actor{Account: s.account, Character: s.character.ID}
}

func (s *Session) mapID() string {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return s.out.mapID
}
