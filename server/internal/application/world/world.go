package world

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"finga/internal/gamedata"
)

const (
	persistQueue    = 4096
	transferTimeout = 5 * time.Second

	// DefaultSaveRetryFor is how long the persistence worker keeps retrying
	// a save that fails transiently before it logs it and moves on.
	DefaultSaveRetryFor = time.Minute
	saveRetryFirst      = 100 * time.Millisecond
	saveRetryMax        = 5 * time.Second
)

// World is the registry: one instance per map, on one channel (B4 adds
// more channels), the persistence worker they share, and the transfers
// between them.
type World struct {
	instances map[string]*Instance
	order     []string
	persist   chan Job
	saver     Saver
	log       *slog.Logger
	workers   *Workers
	retryFor  time.Duration
}

// New builds an instance for every map in data.
func New(data *gamedata.Data, saver Saver, log *slog.Logger, opts Options) *World {
	if opts.SaveRetryFor <= 0 {
		opts.SaveRetryFor = DefaultSaveRetryFor
	}
	w := &World{instances: map[string]*Instance{}, persist: make(chan Job, persistQueue), saver: saver, log: log, workers: opts.Workers, retryFor: opts.SaveRetryFor}
	opts.Transfer = w
	for _, m := range data.Maps() {
		w.instances[m.ID] = NewInstance(m, data, w.persist, opts)
		w.order = append(w.order, m.ID)
	}
	return w
}

// Instance finds a map's actor.
func (w *World) Instance(mapID string) (*Instance, error) {
	i, ok := w.instances[mapID]
	if !ok {
		return nil, ErrNoMap
	}
	return i, nil
}

// Transfer joins the player to another map's instance from its own
// goroutine, so the calling tick never waits. A failed join is logged; the
// player's next Hello puts them back on their saved map.
func (w *World) Transfer(p Player, sink Sink, mapID string) {
	target, ok := w.instances[mapID]
	if !ok {
		if w.log != nil {
			w.log.Error("transfer to an unknown map", "character_id", p.ID, "map_id", mapID)
		}
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), transferTimeout)
		defer cancel()
		if err := target.Join(ctx, p, sink); err != nil && w.log != nil {
			w.log.ErrorContext(ctx, "transfer failed", "character_id", p.ID, "map_id", mapID, "err", err)
		}
	}()
}

// Run ticks every instance and serves the economy workers until ctx is
// done. Jobs queued on the way out are flushed before it returns.
func (w *World) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	if w.workers != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.workers.Run(ctx)
		}()
	}
	for _, id := range w.order {
		wg.Add(1)
		go func(i *Instance) {
			defer wg.Done()
			_ = i.Run(ctx)
		}(w.instances[id])
	}
	wg.Wait()
	// The instances are done, so no more jobs arrive; drain what is queued
	// with a context that is not cancelled.
	w.flush(context.WithoutCancel(ctx))
	return nil
}

// Persist queues a job for the persistence worker without blocking; false
// when the queue is full (the next checkpoint sweep tries again).
func (w *World) Persist(job Job) bool {
	select {
	case w.persist <- job:
		return true
	default:
		return false
	}
}

// RunPersistence saves queued checkpoints and level-ups until ctx is done,
// one at a time and in order: a save that fails transiently is retried with
// backoff for up to SaveRetryFor while the next jobs wait, so an older
// checkpoint never lands after a newer one. Run flushes the queue at the
// end; call this beside Run to save during the run.
func (w *World) RunPersistence(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.persist:
			w.save(ctx, job, w.retryFor)
		}
	}
}

// flush saves what is queued once each, without retrying: on the way out
// a character service that is down must not hold the shutdown.
func (w *World) flush(ctx context.Context) {
	for {
		select {
		case job := <-w.persist:
			w.save(ctx, job, 0)
		default:
			return
		}
	}
}

func (w *World) save(ctx context.Context, job Job, retryFor time.Duration) {
	deadline := time.Now().Add(retryFor)
	wait := saveRetryFirst
	for {
		err := w.saveOnce(ctx, job)
		if err == nil {
			return
		}
		if !w.saver.Retryable(err) || time.Now().Add(wait).After(deadline) {
			if w.log != nil {
				w.log.ErrorContext(ctx, "persistence failed", "err", err)
			}
			return
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		wait = min(wait*2, saveRetryMax)
	}
}

func (w *World) saveOnce(ctx context.Context, job Job) error {
	switch {
	case job.LevelUp != nil:
		return w.saver.SaveLevelUp(ctx, *job.LevelUp)
	case job.Checkpoint != nil:
		return w.saver.SaveCheckpoint(ctx, *job.Checkpoint)
	}
	return nil
}
