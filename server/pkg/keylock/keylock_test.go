package keylock_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/keylock"
)

func TestOneHolderAtATimePerKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := keylock.New[string]()
		unlock, err := l.Lock(t.Context(), "ayla")
		if err != nil {
			t.Fatal(err)
		}
		other, err := l.Lock(t.Context(), "bo")
		if err != nil {
			t.Fatal("another key waited for ayla's holder")
		}
		other()
		got := make(chan struct{})
		go func() {
			next, err := l.Lock(t.Context(), "ayla")
			if err != nil {
				t.Error(err)
				return
			}
			close(got)
			next()
		}()
		synctest.Wait()
		select {
		case <-got:
			t.Fatal("a second holder of ayla while the first held it")
		default:
		}
		unlock()
		unlock() // a second call does nothing
		synctest.Wait()
		select {
		case <-got:
		default:
			t.Fatal("the next holder never got ayla")
		}
	})
}

func TestAWaitEndsWithItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := keylock.New[string]()
		unlock, _ := l.Lock(t.Context(), "ayla")
		defer unlock()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := l.Lock(ctx, "ayla"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err %v, want the wait's deadline", err)
		}
	})
}

// A caller that already gave up still gets a free lock, so work that needs no wait, such as finding nothing to do, runs.
func TestAFreeLockIsTakenEvenWithItsContextDone(t *testing.T) {
	l := keylock.New[string]()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 100 {
		unlock, err := l.Lock(ctx, "ayla")
		if err != nil {
			t.Fatal(err)
		}
		unlock()
	}
}
