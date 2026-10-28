package world

import (
	"context"
	"sync"
)

// EconomyObserver counts jobs in flight; internal/telemetry implements it.
type EconomyObserver interface {
	JobStarted()
	JobFinished()
}

// Workers is the economy worker pool (page 3): a fixed number of goroutines
// that run database transactions for pickups and inventory actions, so a
// tick never waits on the database. Submit never blocks.
type Workers struct {
	jobs chan func(ctx context.Context)
	n    int
	obs  EconomyObserver
}

// DefaultEconomyQueue is how many jobs may wait for a worker.
const DefaultEconomyQueue = 1024

// NewWorkers makes a pool of n workers with a queue of DefaultEconomyQueue.
func NewWorkers(n int, obs EconomyObserver) *Workers {
	if n < 1 {
		n = 1
	}
	if obs == nil {
		obs = noopEconomy{}
	}
	return &Workers{jobs: make(chan func(ctx context.Context), DefaultEconomyQueue), n: n, obs: obs}
}

// Submit queues a job; false when the queue is full.
func (w *Workers) Submit(job func(ctx context.Context)) bool {
	select {
	case w.jobs <- job:
		w.obs.JobStarted()
		return true
	default:
		return false
	}
}

// Run serves jobs until ctx is done.
func (w *Workers) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range w.n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-w.jobs:
					job(ctx)
					w.obs.JobFinished()
				}
			}
		}()
	}
	wg.Wait()
}

type noopEconomy struct{}

// JobStarted does nothing.
func (noopEconomy) JobStarted() {}

// JobFinished does nothing.
func (noopEconomy) JobFinished() {}
