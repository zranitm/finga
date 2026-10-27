// Package hub fans messages out to subscribers keyed by ID, each with a
// bounded queue. A sender never blocks: a subscriber whose queue is full is
// closed instead, and whoever serves it (a stream, say) ends and lets its
// peer subscribe again. Subscribing again under the same ID replaces and
// closes the old subscription, so a sender holding the old one fails from
// then on and never reaches the new one.
package hub

import "sync"

// Hub holds the subscriptions.
type Hub[T any] struct {
	queue int

	mu   sync.Mutex
	subs map[string]*Sub[T]
}

// New makes a hub whose subscriptions queue up to queue messages each.
func New[T any](queue int) *Hub[T] {
	if queue < 1 {
		queue = 1
	}
	return &Hub[T]{queue: queue, subs: map[string]*Sub[T]{}}
}

// Sub is one subscription.
type Sub[T any] struct {
	id   string
	hub  *Hub[T]
	c    chan T
	done chan struct{}

	mu     sync.Mutex // guards closed and sends, so nothing is sent after Done
	closed bool
}

// Subscribe opens a subscription under id, replacing (and closing) any
// older one with the same id.
func (h *Hub[T]) Subscribe(id string) *Sub[T] {
	s := &Sub[T]{id: id, hub: h, c: make(chan T, h.queue), done: make(chan struct{})}
	h.mu.Lock()
	old := h.subs[id]
	h.subs[id] = s
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
	return s
}

// Get is the current subscription under id.
func (h *Hub[T]) Get(id string) (*Sub[T], bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.subs[id]
	return s, ok
}

// Broadcast sends v to every subscription; a full one is closed.
func (h *Hub[T]) Broadcast(v T) {
	h.mu.Lock()
	subs := make([]*Sub[T], 0, len(h.subs))
	for _, s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		s.Send(v)
	}
}

// C is where the subscription's messages arrive.
func (s *Sub[T]) C() <-chan T { return s.c }

// Done is closed when the subscription ends: closed by its owner, replaced
// by a newer one, or overflowed. Messages still in C may be dropped.
func (s *Sub[T]) Done() <-chan struct{} { return s.done }

// Send queues v without blocking. It reports false when the subscription
// has ended, or when its queue is full, which ends it.
func (s *Sub[T]) Send(v T) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	select {
	case s.c <- v:
		s.mu.Unlock()
		return true
	default:
	}
	s.mu.Unlock()
	s.Close()
	return false
}

// Close ends the subscription and removes it from the hub, unless a newer
// one has replaced it there. Closing twice is fine.
func (s *Sub[T]) Close() {
	s.hub.mu.Lock()
	if s.hub.subs[s.id] == s {
		delete(s.hub.subs, s.id)
	}
	s.hub.mu.Unlock()
	s.close()
}

func (s *Sub[T]) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
}
