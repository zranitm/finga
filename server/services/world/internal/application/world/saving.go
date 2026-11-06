package world

import (
	"context"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/services/world/internal/model"
)

// persistence saves with retries while ctx lasts, then gives each checkpoint left one try within the flush budget.
func (w *World) persistence(ctx context.Context, stopped <-chan struct{}) {
	w.saveWhile(ctx)
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.flushTimeout)
	defer cancel()
	var flushers sync.WaitGroup
	for range flushWorkers {
		flushers.Go(func() { w.flush(final, stopped) })
	}
	flushers.Wait()
}

// flush gives each checkpoint left one last try, until the maps have stopped and none is left.
func (w *World) flush(ctx context.Context, stopped <-chan struct{}) {
	for {
		if cp, ok := w.saves.Take(); ok {
			w.lastTry(ctx, cp)
			continue
		}
		select {
		case <-w.saves.Ready():
		case <-stopped:
			for cp, ok := w.saves.Take(); ok; cp, ok = w.saves.Take() {
				w.lastTry(ctx, cp)
			}
			return
		}
	}
}

// saveWhile saves until ctx ends; a failed save retries behind the others, so one failing save holds up no other.
func (w *World) saveWhile(ctx context.Context) {
	wait := &backoff.ExponentialBackOff{InitialInterval: saveRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: saveRetryMax}
	for ctx.Err() == nil {
		cp, ok := w.saves.Take()
		if !ok {
			select {
			case <-w.saves.Ready():
			case <-ctx.Done():
			}
			continue
		}
		err := w.saveOnce(ctx, cp)
		retry := err != nil && w.deps.Saver.Retryable(err)
		w.saves.Done(cp, retry)
		switch {
		case err == nil:
			wait.Reset()
		case !retry:
			w.deps.Logger.ErrorContext(ctx, "persistence failed", append(saveAttrs(cp), "err", err)...)
		case ctx.Err() == nil:
			w.deps.Logger.WarnContext(ctx, "save failed; retrying", append(saveAttrs(cp), "err", err)...)
			pause(ctx, wait.NextBackOff())
		}
	}
}

// lastTry saves cp once on shutdown; a failure is logged, as the save is lost.
func (w *World) lastTry(ctx context.Context, cp model.Checkpoint) {
	if err := w.saveOnce(ctx, cp); err != nil {
		w.deps.Logger.ErrorContext(ctx, "persistence failed", append(saveAttrs(cp), "shutdown", true, "err", err)...)
	}
	w.saves.Done(cp, false)
}

// saveAttrs name a save in the log.
func saveAttrs(cp model.Checkpoint) []any {
	return []any{"character_id", cp.ID, "map_id", cp.MapID, "level", cp.Level}
}

func (w *World) saveOnce(ctx context.Context, cp model.Checkpoint) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.Saver.SaveCheckpoint(ctx, cp)
}

// pause waits d, or until ctx ends.
func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
