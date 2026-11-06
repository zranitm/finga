// Package economy runs the world's calls to the item service off the ticks, on a pool of workers.
package economy

import (
	"context"

	"golang.org/x/sync/errgroup"

	"finga/pkg/panics"
)

// Observer counts jobs in flight.
type Observer interface {
	JobStarted()
	JobFinished()
}

// Workers is the economy worker pool, so a tick never waits on the database.
type Workers struct {
	jobs   chan func(ctx context.Context)
	n      int
	obs    Observer
	panics *panics.Recorder
}

// queue is how many jobs may wait for a worker.
const queue = 1024

// NewWorkers makes a pool of n workers; a job's panic goes to rec.
func NewWorkers(n int, obs Observer, rec *panics.Recorder) *Workers {
	return &Workers{jobs: make(chan func(ctx context.Context), queue), n: n, obs: obs, panics: rec}
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
