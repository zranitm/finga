// Package coalesce is a work queue that keeps only each key's newest value, so it grows with keys and Put never blocks.
package coalesce

import "sync"

// Queue holds each key's newest value until a consumer takes it.
type Queue[K comparable, V any] struct {
	key   func(V) K
	ready chan struct{}

	mu      sync.Mutex
	waiting map[K]V // the newest value put, not yet taken
	taken   map[K]V // handed out, until Done
	line    []K     // the keys waiting and not taken, oldest first
}

// New makes a queue that keys each value with key.
func New[K comparable, V any](key func(V) K) *Queue[K, V] {
	return &Queue[K, V]{key: key, ready: make(chan struct{}, 1), waiting: map[K]V{}, taken: map[K]V{}}
}

// Put makes v its key's newest value; one still waiting is replaced and keeps its place in line.
func (q *Queue[K, V]) Put(v V) {
	k := q.key(v)
	q.mu.Lock()
	_, queued := q.waiting[k]
	_, out := q.taken[k]
	q.waiting[k] = v
	joins := !queued && !out
	if joins {
		q.line = append(q.line, k)
	}
	q.mu.Unlock()
	if joins {
		q.signal()
	}
}

// Take hands out the value that has waited longest, signalling Ready again while more wait; false when there is none.
func (q *Queue[K, V]) Take() (V, bool) {
	q.mu.Lock()
	if len(q.line) == 0 {
		q.mu.Unlock()
		var zero V
		return zero, false
	}
	k := q.line[0]
	q.line = q.line[1:]
	v := q.waiting[k]
	delete(q.waiting, k)
	q.taken[k] = v
	more := len(q.line) > 0
	q.mu.Unlock()
	if more {
		q.signal()
	}
	return v, true
}

// Done gives back v's key from Take; a value put meanwhile joins the end of the line, else v does if retry.
func (q *Queue[K, V]) Done(v V, retry bool) {
	k := q.key(v)
	q.mu.Lock()
	old, out := q.taken[k]
	if !out {
		q.mu.Unlock()
		return
	}
	delete(q.taken, k)
	_, newer := q.waiting[k]
	if !newer && retry {
		q.waiting[k] = old
	}
	queued := newer || retry
	if queued {
		q.line = append(q.line, k)
	}
	q.mu.Unlock()
	if queued {
		q.signal()
	}
}

// Get is the key's newest value, whether waiting or taken.
func (q *Queue[K, V]) Get(k K) (V, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if v, ok := q.waiting[k]; ok {
		return v, true
	}
	v, ok := q.taken[k]
	return v, ok
}

// Ready receives after a value joins the line; consumers whose Take found nothing wait on it.
func (q *Queue[K, V]) Ready() <-chan struct{} { return q.ready }

func (q *Queue[K, V]) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}
