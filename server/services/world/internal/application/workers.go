package application

import (
	"context"
	"log/slog"
	"sync"

	"finga/pkg/panics"
)

// EconomyObserver counts jobs in flight.
type EconomyObserver interface {
	JobStarted()
	JobFinished()
}

// Workers is the economy worker pool, so a tick never waits on the database.
type Workers struct {
	jobs   chan func(ctx context.Context)
	n      int
	obs    EconomyObserver
	panics *panics.Recorder
}

// DefaultEconomyQueue is how many jobs may wait for a worker.
const DefaultEconomyQueue = 1024

// NewWorkers makes a pool of n workers with a queue of DefaultEconomyQueue; a job's panic goes to rec.
func NewWorkers(n int, obs EconomyObserver, rec *panics.Recorder) *Workers {
	if n < 1 {
		n = 1
	}
	if obs == nil {
		obs = noopEconomy{}
	}
	if rec == nil {
		rec = panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
	}
	return &Workers{jobs: make(chan func(ctx context.Context), DefaultEconomyQueue), n: n, obs: obs, panics: rec}
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
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-w.jobs:
					w.panics.Do(ctx, panics.FlowJob, func() { job(ctx) }, "job", "economy")
					w.obs.JobFinished()
				}
			}
		})
	}
	wg.Wait()
}

type noopEconomy struct{}

// JobStarted does nothing.
func (noopEconomy) JobStarted() {}

// JobFinished does nothing.
func (noopEconomy) JobFinished() {}
