package coalesce_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/pkg/coalesce"
)

// A value to retry goes behind the others after a pause, a dropped one is gone, and a handled one resets the pause.
func TestDrainRetriesBehindTheOthers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := coalesce.New(func(v string) string { return v })
		for _, v := range []string{"a", "b", "c"} {
			q.Put(v)
		}
		ctx, cancel := context.WithCancel(t.Context())
		var mu sync.Mutex
		var seen []string
		var at []time.Duration
		start := time.Now()
		tries := map[string]int{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			q.Drain(ctx, backoff.NewConstantBackOff(time.Second), func(v string) coalesce.Outcome {
				mu.Lock()
				defer mu.Unlock()
				seen, at = append(seen, v), append(at, time.Since(start))
				tries[v]++
				switch {
				case v == "a" && tries[v] == 1:
					return coalesce.Retry
				case v == "b":
					return coalesce.Dropped
				default:
					return coalesce.Handled
				}
			})
		}()
		time.Sleep(time.Minute)
		synctest.Wait()
		cancel()
		<-done
		if want := []string{"a", "b", "c", "a"}; !slices.Equal(seen, want) {
			t.Fatalf("handled %v, want %v", seen, want)
		}
		if want := []time.Duration{0, time.Second, time.Second, time.Second}; !slices.Equal(at, want) {
			t.Fatalf("at %v, want %v: one pause, after the retry", at, want)
		}
	})
}

// Drain returns once ctx ends, waiting for nothing to take.
func TestDrainStopsWithItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := coalesce.New(func(v int) int { return v })
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			q.Drain(ctx, backoff.NewConstantBackOff(time.Second), func(int) coalesce.Outcome { return coalesce.Handled })
		}()
		synctest.Wait()
		cancel()
		<-done
	})
}

// A panic in handle reaches Drain's caller, but the key goes back: a consumer that restarts takes the next value put for it.
func TestAPanicInHandleGivesTheKeyBack(t *testing.T) {
	q := coalesce.New(func(v [2]string) string { return v[0] })
	q.Put([2]string{"boss_appeared:forest", "first"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panic did not reach Drain's caller")
			}
		}()
		q.Drain(ctx, &backoff.ZeroBackOff{}, func([2]string) coalesce.Outcome { panic("bad announcement") })
	}()
	q.Put([2]string{"boss_appeared:forest", "second"})
	got := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Drain(ctx, &backoff.ZeroBackOff{}, func(v [2]string) coalesce.Outcome {
			got <- v[1]
			cancel()
			return coalesce.Handled
		})
	}()
	select {
	case v := <-got:
		if v != "second" {
			t.Fatalf("the restarted consumer took %q, want the value put after the panic", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the key stayed taken after the panic, so its next value never queued")
	}
	<-done
}
