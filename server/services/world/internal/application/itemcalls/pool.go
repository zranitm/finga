// Package itemcalls runs the world's calls to the item service off the ticks, on a pool of workers.
package itemcalls

import (
	"context"

	"golang.org/x/sync/errgroup"

	"finga/pkg/panics"
)

// Observer counts the calls queued or running.
type Observer interface {
	CallQueued()
	CallDone()
}

// Pool runs the calls to the item service, so a tick never waits on the database.
type Pool struct {
	jobs   chan func(ctx context.Context)
	n      int
	obs    Observer
	panics *panics.Recorder
}

// queue is how many calls may wait for a worker.
const queue = 1024

// Deps are what the pool reports to; all are required.
type Deps struct {
	Observer Observer
	Panics   *panics.Recorder // a job's panic
}

// Options size the pool.
type Options struct {
	Workers int // calls that run at once
}

// NewPool makes the pool, which Run starts.
func NewPool(d Deps, o Options) *Pool {
	return &Pool{jobs: make(chan func(ctx context.Context), queue), n: o.Workers, obs: d.Observer, panics: d.Panics}
}

// Submit queues a call; false when the queue is full.
func (w *Pool) Submit(job func(ctx context.Context)) bool {
	select {
	case w.jobs <- job:
		w.obs.CallQueued()
		return true
	default:
		return false
	}
}

// Run serves calls until ctx is done; a call's panic is recorded, and one outside a call stops the pool and is returned.
func (w *Pool) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	for range w.n {
		g.Go(func() (err error) {
			defer panics.Capture(&err)
			for {
				select {
				case <-gctx.Done():
					return nil
				case job := <-w.jobs:
					w.panics.Do(gctx, panics.FlowJob, func() { job(gctx) }, "job", "item_call")
					w.obs.CallDone()
				}
			}
		})
	}
	return g.Wait()
}
