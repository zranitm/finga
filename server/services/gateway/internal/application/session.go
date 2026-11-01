package application

import (
	"context"
	"sync"
	"sync/atomic"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

// pendingMax bounds what may wait in a session for its Welcome (or for a
// rejoin to finish): a join takes milliseconds, so this many deliveries
// only pile up when something is wrong, and the connection closes.
const pendingMax = 128

// Session is one connection as the use cases keep it, from Open to Closed:
// who plays on it and what reaches it. Identity is set once by Hello and
// never changed by a command. The WebSocket handler holds it for its
// connection and passes it to every use case.
type Session struct {
	conn Conn
	// ctx ends at Closed, so a rejoin in flight stops.
	ctx      context.Context
	cancel   context.CancelFunc
	isActive atomic.Bool

	// Set by Hello under the gateway's lock, before the session is active.
	account   kernel.AccountID
	sessionID kernel.SessionID
	character model.Player
	closed    bool // Closed ran; a rejoin that finishes after it leaves again

	rejoinMu sync.Mutex // one join at a time: Hello's, then each rejoin

	out out
}

// Character is the character the session plays; zero until Hello joins.
func (s *Session) Character() kernel.CharacterID { return s.character.ID }

func (s *Session) active() bool { return s.isActive.Load() }
func (s *Session) setActive()   { s.isActive.Store(true) }

// out is the session's side of the world's delivery stream: the map the
// player is on (commands carry it), and the order of what reaches the
// connection while a join is in flight. A join's Snapshot comes back on the
// stream, and its answer on another call, in either order: deliveries wait
// until the join's first message (Welcome, or MapChange on a rejoin) is
// out, and anything before the join's Snapshot belongs to an older
// presence of the character and is dropped. Ticks are shifted so they never
// go back on the connection when a restarted world counts from zero again.
type out struct {
	mu       sync.Mutex
	mapID    string
	holding  bool             // a join is in flight: deliveries wait
	awaiting bool             // the join's Snapshot has not arrived yet
	pending  []model.Delivery // held deliveries, in order
	lastTick uint64           // the last tick sent on the connection
	offset   uint64           // added to the world's ticks
	rebase   bool             // the next Snapshot may start the ticks again
	conn     Conn
}

// hold starts a join.
func (o *out) hold() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.awaiting, o.pending, o.rebase = true, true, nil, true
}

// deliver forwards a delivery, holds it while a join is in flight, or
// drops it while the join's Snapshot is still to come. It reports false
// when too much is waiting.
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

// release ends a join that held: first tells the player first (Welcome,
// or a rejoin's MapChange; nil: nothing), then what was held goes out, in
// order; the Snapshot may still be on its way.
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

// emit forwards a delivery with its tick shifted, and follows a move to
// another map. Called with mu held.
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
	o.conn.Deliver(d)
}

// presence routes a command: the character and the map it is on.
func (s *Session) presence() model.Presence {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return model.Presence{Character: s.character.ID, MapID: s.out.mapID}
}

// actor is who acts on items: the account and its joined character.
func (s *Session) actor() model.Actor {
	return model.Actor{Account: s.account, Character: s.character.ID}
}

func (s *Session) mapID() string {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return s.out.mapID
}
