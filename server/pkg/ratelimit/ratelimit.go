// Package ratelimit keeps a golang.org/x/time/rate token bucket per key and evicts idle keys.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is what callers depend on.
type Limiter interface {
	// Allow takes a token from key's bucket and reports whether there was one.
	Allow(key string) bool
}

const (
	idleTTL    = 10 * time.Minute // an untouched key is evicted after this
	sweepEvery = 4096             // Allow calls between sweeps
)

// Buckets is a Limiter over a map of keys. Safe for concurrent use.
type Buckets struct {
	limit rate.Limit
	burst int

	mu    sync.Mutex
	keys  map[string]*entry
	calls int
}

type entry struct {
	limiter *rate.Limiter
	last    time.Time
}

// NewBuckets makes buckets refilling at perSecond tokens a second, up to burst.
func NewBuckets(perSecond float64, burst int) *Buckets {
	return &Buckets{limit: rate.Limit(perSecond), burst: burst, keys: map[string]*entry{}}
}

// Allow takes a token from key's bucket, creating it full.
func (b *Buckets) Allow(key string) bool {
	now := time.Now()
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

// sweep evicts keys idle longer than idleTTL.
func (b *Buckets) sweep(now time.Time) {
	cutoff := now.Add(-idleTTL)
	for key, e := range b.keys {
		if e.last.Before(cutoff) {
			delete(b.keys, key)
		}
	}
}
