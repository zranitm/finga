package ratelimit_test

import (
	"fmt"
	"testing"
	"time"

	"finga/pkg/ratelimit"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock                   { return &clock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)} }
func allowN(b *ratelimit.Buckets, key string, n int) (allowed int) {
	for range n {
		if b.Allow(key) {
			allowed++
		}
	}
	return allowed
}

func TestBurstAndRefill(t *testing.T) {
	tests := []struct {
		name  string
		rate  float64 // per second
		burst int
		steps []struct {
			wait  time.Duration
			tries int
			want  int
		}
	}{
		{"the burst is allowed, then nothing", 1, 3, []struct {
			wait  time.Duration
			tries int
			want  int
		}{{0, 5, 3}}},
		{"one token refills per period", 1, 3, []struct {
			wait  time.Duration
			tries int
			want  int
		}{{0, 3, 3}, {time.Second, 5, 1}, {2 * time.Second, 5, 2}}},
		{"refill never exceeds the burst", 10, 2, []struct {
			wait  time.Duration
			tries int
			want  int
		}{{time.Minute, 5, 2}}},
		{"five per minute, as the login limit", 5.0 / 60, 5, []struct {
			wait  time.Duration
			tries int
			want  int
		}{{0, 6, 5}, {30 * time.Second, 5, 2}, {time.Minute, 10, 5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClock()
			b := ratelimit.NewBuckets(tt.rate, tt.burst, ratelimit.WithClock(c.now))
			for i, step := range tt.steps {
				c.advance(step.wait)
				if got := allowN(b, "k", step.tries); got != step.want {
					t.Fatalf("step %d: allowed %d of %d, want %d", i, got, step.tries, step.want)
				}
			}
		})
	}
}

func TestKeysAreIndependent(t *testing.T) {
	c := newClock()
	b := ratelimit.NewBuckets(1, 2, ratelimit.WithClock(c.now))
	if got := allowN(b, "alice", 5); got != 2 {
		t.Fatalf("alice: %d", got)
	}
	if got := allowN(b, "bob", 5); got != 2 {
		t.Fatalf("bob: %d, alice's bucket leaked", got)
	}
	if b.Len() != 2 {
		t.Fatalf("Len = %d, want 2", b.Len())
	}
}

func TestIdleKeysAreEvicted(t *testing.T) {
	c := newClock()
	b := ratelimit.NewBuckets(1, 1, ratelimit.WithClock(c.now), ratelimit.WithIdleTTL(time.Minute))
	b.Allow("old")
	c.advance(30 * time.Second)
	b.Allow("fresh")
	c.advance(31 * time.Second) // old idle 61 s, fresh idle 31 s
	b.Sweep()
	if b.Len() != 1 {
		t.Fatalf("Len = %d after the sweep, want 1 (fresh only)", b.Len())
	}
	// An evicted key starts over with a full bucket.
	if !b.Allow("old") {
		t.Fatal("old should have a fresh bucket")
	}
	t.Run("Allow sweeps by itself as keys pile up", func(t *testing.T) {
		for i := range 5000 {
			b.Allow(fmt.Sprintf("k%d", i))
		}
		c.advance(2 * time.Minute)
		for i := range 5000 {
			b.Allow(fmt.Sprintf("j%d", i)) // crosses the automatic sweep threshold
		}
		if n := b.Len(); n >= 10000 {
			t.Fatalf("Len = %d: idle keys were never swept", n)
		}
	})
}

func TestConcurrentUse(t *testing.T) {
	c := newClock()
	b := ratelimit.NewBuckets(1000, 1000, ratelimit.WithClock(c.now)) // a frozen clock: no refill during the test
	done := make(chan int)
	for range 8 {
		go func() { done <- allowN(b, "shared", 250) }()
	}
	total := 0
	for range 8 {
		total += <-done
	}
	if total > 1000 || total < 999 {
		t.Fatalf("allowed %d of 2000 with a burst of 1000", total)
	}
}
