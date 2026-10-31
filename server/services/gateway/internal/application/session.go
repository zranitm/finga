package application

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/services/gateway/internal/model"
)

// pendingMax bounds what may wait in a session for its Welcome (or for a
// rejoin to finish): a join takes milliseconds, so this many deliveries
// only pile up when something is wrong, and the connection closes.
const pendingMax = 128

// session is one connection's state. Identity is set once by Hello and
// never changed by a message.
type session struct {
	conn       Conn
	helloTimer *time.Timer
	isActive   atomic.Bool

	// The command queue: the read loop puts messages on it (helloSeen and
	// limiters are the read loop's own), one goroutine drains it with ctx,
	// which Closed cancels, and closes drained when it stops.
	helloSeen bool
	queue     chan model.Message
	ctx       context.Context
	cancel    context.CancelFunc
	drained   chan struct{}

	// Set by Hello under the gateway's lock, before the session is active.
	account   kernel.AccountID
	sessionID kernel.SessionID
	character model.Player
	closed    bool // Closed ran; a rejoin that finishes after it leaves again

	rejoinMu sync.Mutex // one join at a time: Hello's, then each rejoin

	out out

	limiters map[string]*rate.Limiter // by message type; the read loop's own
}

func (s *session) active() bool { return s.isActive.Load() }
func (s *session) setActive()   { s.isActive.Store(true) }

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
	holding  bool            // a join is in flight: deliveries wait
	awaiting bool            // the join's Snapshot has not arrived yet
	pending  []model.Message // held deliveries, in order
	lastTick uint64          // the last tick sent on the connection
	offset   uint64          // added to the world's ticks
	rebase   bool            // the next Snapshot may start the ticks again
	send     func(model.Message)
}

// hold starts a join.
func (o *out) hold() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.awaiting, o.pending, o.rebase = true, true, nil, true
}

// deliver sends a delivery, holds it while a join is in flight, or drops
// it while the join's Snapshot is still to come. It reports false when too
// much is waiting.
func (o *out) deliver(m model.Message) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.awaiting {
		if m.Type != "Snapshot" {
			return true
		}
		o.awaiting = false
	}
	if !o.holding {
		o.emit(m)
		return true
	}
	if len(o.pending) >= pendingMax {
		return false
	}
	o.pending = append(o.pending, m)
	return true
}

// release ends a join that held: first goes out (Welcome, or a rejoin's
// MapChange), then what was held, in order; the Snapshot may still be on
// its way.
func (o *out) release(mapID string, first ...model.Message) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.mapID = mapID
	for _, m := range first {
		o.send(m)
	}
	for _, m := range o.pending {
		o.emit(m)
	}
	o.holding, o.pending = false, nil
}

// abort ends a join that failed: nothing it held, or awaited, is coming.
func (o *out) abort() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holding, o.awaiting, o.pending, o.rebase = false, false, nil, false
}

// emit sends a delivery with its tick shifted, and follows a MapChange.
// Called with mu held.
func (o *out) emit(m model.Message) {
	if o.rebase && m.Type == "Snapshot" {
		if m.Tick+o.offset <= o.lastTick {
			o.offset = o.lastTick + 1 - m.Tick
		}
		o.rebase = false
	}
	m.Tick += o.offset
	o.lastTick = max(o.lastTick, m.Tick)
	if mc, ok := m.Data.(protocol.MapChange); ok {
		o.mapID = mc.MapID
	}
	o.send(m)
}

// presence routes a command: the character and the map it is on.
func (s *session) presence() model.Presence {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return model.Presence{Character: s.character.ID, MapID: s.out.mapID}
}

func (s *session) mapID() string {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return s.out.mapID
}

// allow takes one token from the bucket for this message type (a
// golang.org/x/time/rate limiter, created full). The buckets live on the
// session, so limits are per account and connection.
func (s *session) allow(msgType string, lim model.Limit, now func() time.Time) bool {
	l, ok := s.limiters[msgType]
	if !ok {
		l = rate.NewLimiter(rate.Limit(lim.PerSecond), lim.Burst)
		s.limiters[msgType] = l
	}
	return l.AllowN(now(), 1)
}

func typeName(v any) string {
	if v == nil {
		return ""
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
