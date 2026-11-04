package ratelimit

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

func TestIdleKeysAreEvicted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := NewBuckets(1, 1)
		b.Allow("old")
		time.Sleep(idleTTL / 2)
		b.Allow("fresh")
		time.Sleep(idleTTL/2 + time.Second) // old idle past the TTL, fresh not
		b.sweep(time.Now())
		if _, ok := b.keys["fresh"]; len(b.keys) != 1 || !ok {
			t.Fatalf("keys %v after the sweep, want fresh only", b.keys)
		}
		// An evicted key starts over with a full bucket.
		if !b.Allow("old") {
			t.Fatal("old should have a fresh bucket")
		}
		// Allow sweeps by itself as keys pile up.
		for i := range sweepEvery {
			b.Allow(fmt.Sprintf("k%d", i))
		}
		time.Sleep(2 * idleTTL)
		for i := range sweepEvery {
			b.Allow(fmt.Sprintf("j%d", i)) // crosses the sweep threshold
		}
		if n := len(b.keys); n >= 2*sweepEvery {
			t.Fatalf("%d keys: idle ones were never swept", n)
		}
	})
}
