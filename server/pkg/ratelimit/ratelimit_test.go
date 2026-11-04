package ratelimit_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/ratelimit"
)

// The tests run under synctest: time stands still until a test sleeps.

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
			synctest.Test(t, func(t *testing.T) {
				b := ratelimit.NewBuckets(tt.rate, tt.burst)
				for i, step := range tt.steps {
					time.Sleep(step.wait)
					if got := allowN(b, "k", step.tries); got != step.want {
						t.Fatalf("step %d: allowed %d of %d, want %d", i, got, step.tries, step.want)
					}
				}
			})
		})
	}
}

func TestKeysAreIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := ratelimit.NewBuckets(1, 2)
		if got := allowN(b, "alice", 5); got != 2 {
			t.Fatalf("alice: %d", got)
		}
		if got := allowN(b, "bob", 5); got != 2 {
			t.Fatalf("bob: %d, alice's bucket leaked", got)
		}
	})
}

func TestConcurrentUse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := ratelimit.NewBuckets(1000, 1000) // no refill: time stands still
		done := make(chan int)
		for range 8 {
			go func() { done <- allowN(b, "shared", 250) }()
		}
		total := 0
		for range 8 {
			total += <-done
		}
		if total != 1000 {
			t.Fatalf("allowed %d of 2000 with a burst of 1000", total)
		}
	})
}
