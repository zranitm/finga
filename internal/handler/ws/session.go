package ws

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"finga/internal/application/character"
	"finga/internal/model"
	"finga/pkg/ratelimit"
	"finga/pkg/wsconn"
)

// session is one connection's state. Identity is set once by Hello and
// never changed by a message.
type session struct {
	conn       wsconn.Conn
	helloTimer *time.Timer
	isActive   atomic.Bool

	account   model.AccountID
	sessionID model.SessionID
	character character.Character
	instMu    sync.Mutex
	instance  MapInstance

	limiters map[string]*ratelimit.Buckets
}

func (s *session) active() bool { return s.isActive.Load() }
func (s *session) setActive()   { s.isActive.Store(true) }

// limit is a per-message-type token bucket setting (page 4).
type limit struct {
	perSecond float64
	burst     float64
}

// rateLimits are page 4's per-account limits, keyed by message type.
func rateLimits() map[string]limit {
	l := map[string]limit{
		"Input":    {20, 20},
		"Attack":   {10, 10},
		"PickUp":   {10, 10},
		"ChatSend": {1, 3},
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

// inst is the map instance the session is on; it changes on a transfer.
func (s *session) inst() MapInstance {
	s.instMu.Lock()
	defer s.instMu.Unlock()
	return s.instance
}

func (s *session) setInstance(i MapInstance) {
	s.instMu.Lock()
	defer s.instMu.Unlock()
	s.instance = i
}
