package ws

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/model"
	"finga/internal/protocol"
	"finga/pkg/ratelimit"
	"finga/pkg/wsconn"
)

// pendingMax bounds what may wait in a session for its Welcome (or for a
// rejoin to finish): a join takes milliseconds, so this many deliveries
// only pile up when something is wrong, and the connection closes.
const pendingMax = 128

// session is one connection's state. Identity is set once by Hello and
// never changed by a message.
type session struct {
	conn       wsconn.Conn
	helloTimer *time.Timer
	isActive   atomic.Bool

	// The command queue: the read loop puts messages on it (helloSeen and
	// limiters are the read loop's own), one goroutine drains it with ctx,
	// which OnClose cancels, and closes drained when it stops.
	helloSeen bool
	queue     chan wsconn.Message
	ctx       context.Context
	cancel    context.CancelFunc
	drained   chan struct{}

	// Set by Hello under the gateway's lock, before the session is active.
	account   model.AccountID
	sessionID model.SessionID
	character player
	closed    bool // OnClose ran; a rejoin that finishes after it leaves again

	rejoinMu sync.Mutex // one join at a time: Hello's, then each rejoin

	out out

	limiters map[string]*ratelimit.Buckets
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
	holding  bool             // a join is in flight: deliveries wait
	awaiting bool             // the join's Snapshot has not arrived yet
	pending  []wsconn.Message // held deliveries, in order
	lastTick uint64           // the last tick sent on the connection
	offset   uint64           // added to the world's ticks
	rebase   bool             // the next Snapshot may start the ticks again
	send     func(wsconn.Message)
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
func (o *out) deliver(m wsconn.Message) bool {
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
func (o *out) release(mapID string, first ...wsconn.Message) {
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
func (o *out) emit(m wsconn.Message) {
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
func (s *session) presence() *worldv1.Presence {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return &worldv1.Presence{CharacterId: s.character.ID.String(), MapId: s.out.mapID}
}

func (s *session) mapID() string {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return s.out.mapID
}

// player is who the session plays, as Hello joined it: what routing, chat
// and the social service need.
type player struct {
	ID    model.CharacterID
	Name  string
	Level int
	HP    int
	MaxHP int
}

// limit is a per-message-type token bucket setting (page 4).
type limit struct {
	perSecond float64
	burst     float64
}

// rateLimits are page 4's per-account limits, keyed by message type.
// ChatSend is missing on purpose: the chat service limits it (1 per
// second, burst 3) and answers rate_limited, which a silent drop here
// would hide.
func rateLimits() map[string]limit {
	l := map[string]limit{
		"Input":  {30, 30}, // one per tick is 20 a second; the headroom absorbs a burst after a stall
		"Attack": {10, 10},
		"PickUp": {10, 10},
	}
	for _, t := range []string{"PartyInvite", "PartyRespond", "PartyLeave", "PartyKick", "PartyPromote"} {
		l[t] = limit{2, 2}
	}
	for _, t := range []string{"InventoryMove", "InventoryDiscard"} {
		l[t] = limit{5, 5}
	}
	return l
}

// allow charges one token from the bucket for this message type. The
// buckets live on the session, so limits are per account and connection.
func (s *session) allow(msgType string, lim limit, now func() time.Time) bool {
	b, ok := s.limiters[msgType]
	if !ok {
		b = ratelimit.NewBuckets(lim.perSecond, lim.burst, ratelimit.WithClock(now))
		s.limiters[msgType] = b
	}
	return b.Allow(msgType, 1)
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

func isCloseError(err error, target **wsconn.CloseError) bool {
	return errors.As(err, target)
}

func errText(err error) string {
	if err == nil {
		return "peer closed"
	}
	return err.Error()
}
