package world

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/kernel"
	"finga/modules/world/internal/model"
	"finga/pkg/coalesce"
	"finga/pkg/panics"
)

// persistence saves with retries until the maps have stopped, then retries each checkpoint left until it lands or the flush budget ends; a flusher's panic is returned.
func (w *World) persistence() error {
	w.saveWhile(w.mapsCtx)
	flushCtx, cancel := context.WithTimeout(context.Background(), w.flushTimeout)
	defer cancel()
	failed := make([]error, flushWorkers)
	var flushers sync.WaitGroup
	for i := range flushWorkers {
		flushers.Go(func() {
			defer panics.Capture(&failed[i])
			for cp, ok := w.saves.Take(); ok; cp, ok = w.saves.Take() {
				w.lastTries(flushCtx, cp)
			}
		})
	}
	flushers.Wait()
	return errors.Join(failed...)
}

// saveWhile saves until ctx ends; a failed save retries behind the others, so one failing save holds up no other.
func (w *World) saveWhile(ctx context.Context) {
	wait := &backoff.ExponentialBackOff{InitialInterval: saveRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: saveRetryMax}
	unexplained := map[kernel.CharacterID]time.Time{} // since when each character's saves fail without a reason
	w.saves.Drain(ctx, wait, func(cp model.Checkpoint) coalesce.Outcome {
		err := w.saveOnce(ctx, cp)
		drop := w.dropFor(err, cp.ID, unexplained)
		if err == nil || drop != "" {
			delete(unexplained, cp.ID)
		}
		switch {
		case err == nil:
			return coalesce.Handled
		case drop != "":
			w.dropped(ctx, cp, drop, err)
			return coalesce.Dropped
		case ctx.Err() == nil:
			w.deps.Logger.WarnContext(ctx, "save failed; retrying", append(saveAttrs(cp), "err", err)...)
		}
		return coalesce.Retry
	})
}

// dropFor is why a failed save is given up on, or "" to retry it: a refusal at once, a failure without a reason once it has lasted unexplainedFor.
func (w *World) dropFor(err error, id kernel.CharacterID, unexplained map[kernel.CharacterID]time.Time) string {
	switch {
	case err == nil, w.deps.Saver.Retryable(err):
		return ""
	case w.deps.Saver.Refused(err):
		return DropRefused
	}
	since, ok := unexplained[id]
	if !ok {
		since = time.Now()
		unexplained[id] = since
	}
	if time.Since(since) < unexplainedFor {
		return ""
	}
	return DropUnexplained
}

// dropped logs and counts a save given up on: the character's progress since their last save is lost.
func (w *World) dropped(ctx context.Context, cp model.Checkpoint, reason string, err error) {
	w.deps.SaveObserver.SaveDropped(reason)
	w.deps.Logger.ErrorContext(ctx, "persistence failed", append(saveAttrs(cp), "dropped", reason, "err", err)...)
}

// lastTries saves cp once the maps have stopped, retrying until it lands, is refused or ctx, the flush budget, ends; a lost save is logged and counted.
func (w *World) lastTries(ctx context.Context, cp model.Checkpoint) {
	defer w.saves.Done(cp, false)
	wait := &backoff.ExponentialBackOff{InitialInterval: saveRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: saveRetryMax}
	wait.Reset()
	for {
		err := w.saveOnce(ctx, cp)
		switch {
		case err == nil:
			return
		case w.deps.Saver.Refused(err):
			w.dropped(ctx, cp, DropRefused, err)
			return
		case ctx.Err() != nil:
			w.dropped(ctx, cp, DropShutdown, err)
			return
		}
		t := time.NewTimer(wait.NextBackOff())
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
		}
	}
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
