package application

import (
	"context"

	"golang.org/x/sync/errgroup"

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

// economyQueue is how many jobs may wait for a worker.
const economyQueue = 1024

// NewWorkers makes a pool of n workers; a job's panic goes to rec.
func NewWorkers(n int, obs EconomyObserver, rec *panics.Recorder) *Workers {
	return &Workers{jobs: make(chan func(ctx context.Context), economyQueue), n: n, obs: obs, panics: rec}
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

// Run serves jobs until ctx is done; a job's panic is recorded, and one outside a job stops the pool and is returned.
func (w *Workers) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	for range w.n {
		g.Go(func() (err error) {
			defer panics.Capture(&err)
			for {
				select {
				case <-gctx.Done():
					return nil
				case job := <-w.jobs:
					w.panics.Do(gctx, panics.FlowJob, func() { job(gctx) }, "job", "economy")
					w.obs.JobFinished()
				}
			}
		})
	}
	return g.Wait()
}
