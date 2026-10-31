// Package ratelimit is a token bucket per key (an account, an IP, a
// username) on golang.org/x/time/rate, the Go team's limiter. The bucket
// is the library's; this package adds what it leaves to the caller: a
// limiter per key, each created full, and the eviction of keys left
// untouched, so the map does not grow with every visitor ever.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is what callers depend on.
type Limiter interface {
	// Allow takes one token from key's bucket and reports whether there
	// was one.
	Allow(key string) bool
}

// Option tunes NewBuckets.
type Option func(*Buckets)

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) Option {
	return func(b *Buckets) { b.now = now }
}

// WithIdleTTL sets how long an untouched key stays before a sweep evicts it.
func WithIdleTTL(d time.Duration) Option {
	return func(b *Buckets) { b.idleTTL = d }
}

const (
	defaultIdleTTL = 10 * time.Minute
	sweepEvery     = 4096 // Allow calls between automatic sweeps
)

// Buckets is a Limiter over a map of keys. Safe for concurrent use.
type Buckets struct {
	limit   rate.Limit
	burst   int
	idleTTL time.Duration
	now     func() time.Time

	mu    sync.Mutex
	keys  map[string]*entry
	calls int
}

type entry struct {
	limiter *rate.Limiter
	last    time.Time // when the key was last asked, for eviction
}

// NewBuckets makes buckets that refill at perSecond tokens a second up to
// burst.
func NewBuckets(perSecond float64, burst int, opts ...Option) *Buckets {
	b := &Buckets{limit: rate.Limit(perSecond), burst: burst, idleTTL: defaultIdleTTL, now: time.Now, keys: map[string]*entry{}}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Allow takes one token from key's bucket and reports whether there was
// one.
func (b *Buckets) Allow(key string) bool {
	now := b.now()
	b.mu.Lock()
	b.calls++
	if b.calls >= sweepEvery {
		b.calls = 0
		b.sweep(now)
	}
	e, ok := b.keys[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(b.limit, b.burst)}
		b.keys[key] = e
	}
	e.last = now
	b.mu.Unlock()
	return e.limiter.AllowN(now, 1)
}

// Len is the number of keys held.
func (b *Buckets) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.keys)
}

// Sweep evicts keys untouched for longer than the idle TTL. Allow also
// sweeps on its own every few thousand calls.
func (b *Buckets) Sweep() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep(b.now())
}

func (b *Buckets) sweep(now time.Time) {
	cutoff := now.Add(-b.idleTTL)
	for key, e := range b.keys {
		if e.last.Before(cutoff) {
			delete(b.keys, key)
		}
	}
}
