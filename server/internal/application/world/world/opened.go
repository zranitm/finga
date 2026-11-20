package world

import (
	"context"
	"fmt"
	"sync"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/pkg/panics"
)

// opened are the instances the World opens on demand, matches and a region's channels past the first, each on its own goroutine under the World's run: a match closes itself, a channel is closed.
type opened struct {
	mu    sync.Mutex
	by    map[string]*instance.Instance
	close map[string]context.CancelFunc
	ctx   context.Context // Run's while it runs: nil before, and done after
	fail  context.CancelCauseFunc
	runs  sync.WaitGroup
	err   error // the first failure, a panic, which stops the World as a map's does
}

// start lets instances open under ctx until it's done; fail stops the World.
func (o *opened) start(ctx context.Context, fail context.CancelCauseFunc) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.by, o.close, o.ctx, o.fail = map[string]*instance.Instance{}, map[string]context.CancelFunc{}, ctx, fail
}

// run starts inst under id; ErrStopped when the World isn't running.
func (o *opened) run(id string, inst *instance.Instance) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ctx == nil || o.ctx.Err() != nil {
		return model.ErrStopped
	}
	if _, dup := o.by[id]; dup {
		return fmt.Errorf("world: instance %s is open already", id)
	}
	ctx, cancel := context.WithCancel(o.ctx)
	o.by[id], o.close[id] = inst, cancel
	o.runs.Go(func() {
		defer cancel()
		err := func() (err error) {
			defer panics.Capture(&err)
			return inst.Run(ctx)
		}()
		o.mu.Lock()
		defer o.mu.Unlock()
		delete(o.by, id)
		delete(o.close, id)
		if err != nil && o.err == nil {
			o.err = fmt.Errorf("instance %s: %w", id, err)
			o.fail(o.err)
		}
	})
	return nil
}

// get is the open instance id.
func (o *opened) get(id string) (*instance.Instance, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	inst, ok := o.by[id]
	return inst, ok
}

// shut stops the open instance id, as its World's stop would.
func (o *opened) shut(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if cancel, ok := o.close[id]; ok {
		cancel()
	}
}

// stop opens no more, and waits for those open to stop; it returns the first failure.
func (o *opened) stop() error {
	o.mu.Lock()
	o.ctx = nil
	o.mu.Unlock()
	o.runs.Wait()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}
