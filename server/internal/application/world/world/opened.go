package world

import (
	"context"
	"fmt"
	"slices"
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
	ctx   context.Context // Run's while it runs: nil before and after
	fail  context.CancelCauseFunc
	begun bool      // start has been called
	ended bool      // stop has been called: nothing opens again
	later []opening // those opened before start, which start runs
	runs  sync.WaitGroup
	err   error // the first failure, a panic, which stops the World as a map's does
}

// opening is an instance asked to open before the World ran, which its joins wait on in its inbox.
type opening struct {
	id    string
	inst  *instance.Instance
	ended func()
}

// start lets instances open under ctx until it's done, and runs those opened before it; fail stops the World.
func (o *opened) start(ctx context.Context, fail context.CancelCauseFunc) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ctx, o.fail, o.begun = ctx, fail, true
	for _, p := range o.later {
		o.launch(p)
	}
	o.later = nil
}

// run starts inst under id, and calls ended, if any, once it has stopped; before the World runs it waits, so a join to it lands once it does, and ErrStopped once the World has stopped.
func (o *opened) run(id string, inst *instance.Instance, ended func()) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ended || o.begun && o.ctx.Err() != nil {
		return model.ErrStopped
	}
	if _, dup := o.by[id]; dup {
		return fmt.Errorf("world: instance %s is open already", id)
	}
	if o.by == nil {
		o.by, o.close = map[string]*instance.Instance{}, map[string]context.CancelFunc{}
	}
	p := opening{id, inst, ended}
	if !o.begun {
		o.by[id] = inst
		o.later = append(o.later, p)
		return nil
	}
	o.launch(p)
	return nil
}

// launch runs p's instance under the World's run, with o.mu held.
func (o *opened) launch(p opening) {
	ctx, cancel := context.WithCancel(o.ctx)
	o.by[p.id], o.close[p.id] = p.inst, cancel
	o.runs.Go(func() {
		defer cancel()
		err := func() (err error) {
			defer panics.Capture(&err)
			return p.inst.Run(ctx)
		}()
		o.forget(p.id, p.inst, err)
		if p.ended != nil {
			p.ended() // outside o.mu: the channels' lock comes first
		}
	})
}

// forget drops inst, run under id, once it has stopped, unless shut has already, and keeps the first failure, stopping the World.
func (o *opened) forget(id string, inst *instance.Instance, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.by[id] == inst { // shut forgets it at once, and its id may be open again
		delete(o.by, id)
		delete(o.close, id)
	}
	if err != nil && o.err == nil {
		o.err = fmt.Errorf("instance %s: %w", id, err)
		o.fail(o.err)
	}
}

// get is the open instance id.
func (o *opened) get(id string) (*instance.Instance, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	inst, ok := o.by[id]
	return inst, ok
}

// shut stops the open instance id, as its World's stop would, and forgets it at once, so its id can open again while it winds down.
func (o *opened) shut(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if cancel, ok := o.close[id]; ok {
		cancel()
		delete(o.by, id)
		delete(o.close, id)
	}
	if i := slices.IndexFunc(o.later, func(p opening) bool { return p.id == id }); i >= 0 { // never started: run once under a done ctx, so it stops and its waiting joins get ErrStopped
		p := o.later[i]
		o.later = slices.Delete(o.later, i, i+1)
		delete(o.by, id)
		done, stop := context.WithCancel(context.Background())
		stop()
		_ = p.inst.Run(done)
	}
}

// stop opens no more, and waits for those open to stop; it returns the first failure.
func (o *opened) stop() error {
	o.mu.Lock()
	o.ctx, o.ended = nil, true
	o.mu.Unlock()
	o.runs.Wait()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}
