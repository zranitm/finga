// Package ratelimit is keyed token buckets: each key (an account, an IP, a
// username) has its own bucket that refills at a fixed rate up to a burst.
// Idle keys are evicted, so the map doesn't grow with every visitor ever.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter is what callers depend on.
type Limiter interface {
	// Allow charges cost from key's bucket and reports whether it fit.
	Allow(key string, cost float64) bool
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
	rate    float64 // tokens per second
	burst   float64
	idleTTL time.Duration
	now     func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	calls   int
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewBuckets makes buckets that refill at ratePerSecond up to burst.
func NewBuckets(ratePerSecond, burst float64, opts ...Option) *Buckets {
	b := &Buckets{rate: ratePerSecond, burst: burst, idleTTL: defaultIdleTTL, now: time.Now, buckets: map[string]*bucket{}}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Allow charges cost from key's bucket and reports whether it fit. A cost
// above the burst never fits and is never charged.
func (b *Buckets) Allow(key string, cost float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.calls >= sweepEvery {
		b.calls = 0
		b.sweep()
	}
	now := b.now()
	bk, ok := b.buckets[key]
	if !ok {
		bk = &bucket{tokens: b.burst, last: now}
		b.buckets[key] = bk
	} else {
		bk.tokens = min(b.burst, bk.tokens+now.Sub(bk.last).Seconds()*b.rate)
		bk.last = now
	}
	if cost > bk.tokens {
		return false
	}
	bk.tokens -= cost
	return true
}

// Len is the number of keys held.
func (b *Buckets) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buckets)
}

// Sweep evicts keys untouched for longer than the idle TTL. Allow also
// sweeps on its own every few thousand calls.
func (b *Buckets) Sweep() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
}

func (b *Buckets) sweep() {
	cutoff := b.now().Add(-b.idleTTL)
	for key, bk := range b.buckets {
		if bk.last.Before(cutoff) {
			delete(b.buckets, key)
		}
	}
}
