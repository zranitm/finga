package world

import (
	"context"
	"log/slog"
	"sync"

	"finga/internal/application/character"
	"finga/internal/application/gamedata"
)

// persistQueue is how many checkpoints may wait for the worker.
const persistQueue = 4096

// World is the registry: one instance per map, on one channel (B4 adds
// more channels), and the persistence worker they share.
type World struct {
	instances map[string]*Instance
	order     []string
	persist   chan character.Checkpoint
	saver     Saver
	log       *slog.Logger
}

// New builds an instance for every map in data.
func New(data *gamedata.Data, saver Saver, log *slog.Logger, opts Options) *World {
	w := &World{instances: map[string]*Instance{}, persist: make(chan character.Checkpoint, persistQueue), saver: saver, log: log}
	for _, m := range data.Maps() {
		w.instances[m.ID] = NewInstance(m, data.Movement(), w.persist, opts)
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

// Run ticks every instance and runs the persistence worker until ctx is
// done. Checkpoints queued on the way out are flushed before it returns.
func (w *World) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, id := range w.order {
		wg.Add(1)
		go func(i *Instance) {
			defer wg.Done()
			_ = i.Run(ctx)
		}(w.instances[id])
	}
	wg.Wait()
	// The instances are done, so no more checkpoints arrive; drain what is
	// queued with a context that is not cancelled.
	w.flush(context.WithoutCancel(ctx))
	return nil
}

// RunPersistence saves queued checkpoints until ctx is done. Run calls it
// implicitly through flush at the end; call it beside Run to save during
// the run.
func (w *World) RunPersistence(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case cp := <-w.persist:
			w.save(ctx, cp)
		}
	}
}

func (w *World) flush(ctx context.Context) {
	for {
		select {
		case cp := <-w.persist:
			w.save(ctx, cp)
		default:
			return
		}
	}
}

func (w *World) save(ctx context.Context, cp character.Checkpoint) {
	if err := w.saver.SaveCheckpoint(ctx, cp); err != nil && w.log != nil {
		w.log.ErrorContext(ctx, "checkpoint failed", "character_id", cp.ID, "map_id", cp.MapID, "err", err)
	}
}
