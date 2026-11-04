// Package application runs the map instances: one actor per map, ticking at the configured rate.
// Services are called from worker goroutines, never inside a tick.
package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/sync/errgroup"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

const (
	persistQueue    = 4096
	transferTimeout = 5 * time.Second

	saveRetryFor   = time.Minute // how long a transiently failing save is retried
	saveRetryFirst = 100 * time.Millisecond
	saveRetryMax   = 5 * time.Second
)

// World is the registry of map instances, their shared persistence worker and transfers.
type World struct {
	data      *gamedata.Data
	opts      Options
	instances map[string]*Instance
	order     []string
	persist   chan model.Job
	saver     Saver
	log       *slog.Logger
	workers   *Workers
}

// New builds an instance for every map in data.
func New(data *gamedata.Data, saver Saver, log *slog.Logger, opts Options) *World {
	w := &World{data: data, opts: opts, instances: map[string]*Instance{}, persist: make(chan model.Job, persistQueue), saver: saver, log: log, workers: opts.Workers}
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
		return nil, model.ErrNoMap
	}
	return i, nil
}

// Join loads the account's character, grants its starter items and joins its saved map.
func (w *World) Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink Sink) (model.View, error) {
	c, err := w.opts.Characters.Load(ctx, id, account)
	if err != nil {
		return model.View{}, err
	}
	if err := w.opts.Starter.GrantStarter(ctx, account, id, c.Class); err != nil {
		return model.View{}, fmt.Errorf("starter items: %w", err)
	}
	inst, err := w.Instance(c.MapID)
	if err != nil {
		return model.View{}, err
	}
	formulas, ok := w.data.Formulas(c.Class)
	if !ok {
		return model.View{}, model.ErrClassUnavailable
	}
	derived := progression.DeriveStats(formulas, c.Stats, c.Level, progression.Equipment{})
	p := model.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats, HP: c.HP, MP: c.MP, X: c.X, Y: c.Y, Connection: conn}
	if err := inst.Join(ctx, p, sink); err != nil {
		return model.View{}, err
	}
	return model.View{
		ID: c.ID, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats,
		HP: min(c.HP, derived.MaxHP), MaxHP: derived.MaxHP, MP: min(c.MP, derived.MaxMP), MaxMP: derived.MaxMP,
		MapID: c.MapID, X: c.X, Y: c.Y,
	}, nil
}

// Transfer joins the player to another map on its own goroutine, so the calling tick never waits.
func (w *World) Transfer(p model.Player, sink Sink, mapID string) {
	target, ok := w.instances[mapID]
	if !ok {
		if w.log != nil {
			w.log.Error("transfer to an unknown map", "character_id", p.ID, "map_id", mapID)
		}
		return
	}
	w.opts.Panics.Go(context.Background(), panics.FlowJob, func() {
		ctx, cancel := context.WithTimeout(context.Background(), transferTimeout)
		defer cancel()
		if err := target.Join(ctx, p, sink); err != nil && w.log != nil {
			w.log.ErrorContext(ctx, "transfer failed", "character_id", p.ID, "map_id", mapID, "err", err)
		}
	}, "job", "transfer", "character_id", p.ID, "map_id", mapID)
}

// Run ticks every instance and the economy workers until ctx is done, then flushes queued jobs.
// A map that panics stops them all; the others still checkpoint, but its own state is suspect and is not saved.
func (w *World) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return w.workers.Run(gctx) })
	for _, id := range w.order {
		inst := w.instances[id]
		g.Go(func() (err error) {
			defer func() {
				if err != nil {
					err = fmt.Errorf("map %s: %w", id, err)
				}
			}()
			defer panics.Capture(&err)
			return inst.Run(gctx)
		})
	}
	err := g.Wait()
	// No more jobs can arrive; drain the queue with a context that is not cancelled.
	w.flush(context.WithoutCancel(ctx))
	return err
}

// RunPersistence saves queued jobs in order, so an older checkpoint never lands after a newer one.
func (w *World) RunPersistence(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.persist:
			w.save(ctx, job, saveRetryFor)
		}
	}
}

// flush saves each queued job once, so a down character service can't hold the shutdown.
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

func (w *World) save(ctx context.Context, job model.Job, retryFor time.Duration) {
	opts := []backoff.RetryOption{
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: saveRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: saveRetryMax}),
		backoff.WithMaxElapsedTime(retryFor),
	}
	if retryFor <= 0 {
		opts = append(opts, backoff.WithMaxTries(1))
	}
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := w.saveOnce(ctx, job)
		if err != nil && !w.saver.Retryable(err) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	}, opts...)
	if err != nil && ctx.Err() == nil && w.log != nil {
		w.log.ErrorContext(ctx, "persistence failed", "err", err)
	}
}

func (w *World) saveOnce(ctx context.Context, job model.Job) error {
	switch {
	case job.LevelUp != nil:
		return w.saver.SaveLevelUp(ctx, *job.LevelUp)
	case job.Checkpoint != nil:
		return w.saver.SaveCheckpoint(ctx, *job.Checkpoint)
	}
	return nil
}
