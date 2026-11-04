package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestIdleKeysAreEvicted(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	b := NewBuckets(1, 1, WithClock(func() time.Time { return now }))
	b.Allow("old")
	now = now.Add(idleTTL / 2)
	b.Allow("fresh")
	now = now.Add(idleTTL/2 + time.Second) // old idle past the TTL, fresh not
	b.sweep(now)
	if _, ok := b.keys["fresh"]; len(b.keys) != 1 || !ok {
		t.Fatalf("keys %v after the sweep, want fresh only", b.keys)
	}
	// An evicted key starts over with a full bucket.
	if !b.Allow("old") {
		t.Fatal("old should have a fresh bucket")
	}
	t.Run("Allow sweeps by itself as keys pile up", func(t *testing.T) {
		for i := range sweepEvery {
			b.Allow(fmt.Sprintf("k%d", i))
		}
		now = now.Add(2 * idleTTL)
		for i := range sweepEvery {
			b.Allow(fmt.Sprintf("j%d", i)) // crosses the sweep threshold
		}
		if n := len(b.keys); n >= 2*sweepEvery {
			t.Fatalf("%d keys: idle ones were never swept", n)
		}
	})
}
