package tick_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/tick"
)

type stepper struct {
	mu    sync.Mutex
	ticks []uint64
	dts   []time.Duration
	delay time.Duration // how long each Step takes
}

func (s *stepper) Step(tick uint64, dt time.Duration) {
	s.mu.Lock()
	s.ticks = append(s.ticks, tick)
	s.dts = append(s.dts, dt)
	s.mu.Unlock()
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
}

type observation struct {
	tick           uint64
	took, interval time.Duration
}

func TestRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &stepper{}
		var mu sync.Mutex
		var seen []observation
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- tick.Run(ctx, 50*time.Millisecond, s, tick.WithObserver(func(n uint64, took, interval time.Duration) {
				mu.Lock()
				seen = append(seen, observation{n, took, interval})
				mu.Unlock()
			}))
		}()
		time.Sleep(time.Second)
		synctest.Wait()
		s.mu.Lock()
		if len(s.ticks) != 20 {
			t.Fatalf("%d steps in one second at 20 Hz, want 20", len(s.ticks))
		}
		for i, n := range s.ticks {
			if n != uint64(i+1) || s.dts[i] != 50*time.Millisecond {
				t.Fatalf("step %d: tick %d dt %v", i, n, s.dts[i])
			}
		}
		s.mu.Unlock()
		mu.Lock()
		if len(seen) != 20 || seen[0].tick != 1 || seen[0].interval != 50*time.Millisecond || seen[0].took != 0 {
			t.Fatalf("observations %+v", seen[:min(3, len(seen))])
		}
		mu.Unlock()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run returned %v after cancellation", err)
		}
		synctest.Wait()
		s.mu.Lock()
		n := len(s.ticks)
		s.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		s.mu.Lock()
		if len(s.ticks) != n {
			t.Fatal("steps continued after cancellation")
		}
		s.mu.Unlock()
	})
}

func TestSlowStepIsReportedAsAnOverrun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &stepper{delay: 80 * time.Millisecond} // longer than the 50 ms interval
		var mu sync.Mutex
		var seen []observation
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = tick.Run(ctx, 50*time.Millisecond, s, tick.WithObserver(func(n uint64, took, interval time.Duration) {
				mu.Lock()
				seen = append(seen, observation{n, took, interval})
				mu.Unlock()
			}))
		}()
		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		if len(seen) == 0 {
			t.Fatal("no observations")
		}
		for _, o := range seen {
			if o.took != 80*time.Millisecond || o.took <= o.interval {
				t.Fatalf("observation %+v: took should exceed the interval", o)
			}
		}
		// A late step does not pile up extra steps: the ticker drops them.
		if len(seen) > 7 {
			t.Fatalf("%d steps in 500 ms with 80 ms steps; expected about 6", len(seen))
		}
	})
}

func TestRunWithoutObserver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &stepper{}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		defer cancel()
		if err := tick.Run(ctx, 50*time.Millisecond, s); err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.ticks) != 2 {
			t.Fatalf("%d steps, want 2", len(s.ticks))
		}
	})
}

func TestInvalidInterval(t *testing.T) {
	if err := tick.Run(context.Background(), 0, &stepper{}); err == nil {
		t.Fatal("a zero interval was accepted")
	}
}
