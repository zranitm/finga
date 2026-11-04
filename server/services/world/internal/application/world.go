// Package application runs the map instances: one actor per map, ticking at the configured rate.
// Services are called from worker goroutines, never inside a tick.
package application

import (
	"context"
	"errors"
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
	callTimeout     = 5 * time.Second // one call to another service; past it the call counts as failed

	saveRetryFor   = time.Minute // how long a transiently failing save is retried
	saveRetryFirst = 100 * time.Millisecond
	saveRetryMax   = 5 * time.Second
)

// World is the registry of map instances and of where each joined character is, their shared
// persistence worker and transfers.
type World struct {
	data      *gamedata.Data
	deps      WorldDeps
	instances map[string]*Instance
	order     []string
	presences presences
	persist   chan model.Job
}

// WorldDeps are the services the World calls besides its instances'; all are required.
type WorldDeps struct {
	Deps
	Saver      Saver
	Characters Characters
	Starter    Starter
	Panics     *panics.Recorder // a transfer's panic
	Logger     *slog.Logger
}

// New builds an instance for every map in data; the World moves their players between maps.
func New(data *gamedata.Data, t Tuning, d WorldDeps) *World {
	w := &World{
		data: data, deps: d, instances: map[string]*Instance{}, presences: presences{by: map[kernel.CharacterID]presence{}},
		persist: make(chan model.Job, persistQueue),
	}
	for _, m := range data.Maps() {
		w.instances[m.ID] = NewInstance(m, data, w.persist, w, t, d.Deps)
		w.order = append(w.order, m.ID)
	}
	return w
}

// instance finds a map's actor.
func (w *World) instance(mapID string) (*Instance, error) {
	i, ok := w.instances[mapID]
	if !ok {
		return nil, model.ErrNoMap
	}
	return i, nil
}

// Join loads the account's character, grants its starter items (best effort: a failure is logged, and
// the next join tries again) and joins its saved map through conn.
// An older presence of the character on another map leaves it; one on the same map is replaced there.
func (w *World) Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink Sink) (model.View, error) {
	c, err := w.deps.Characters.Load(ctx, id, account)
	if err != nil {
		return model.View{}, err
	}
	if err := w.deps.Starter.GrantStarter(ctx, account, id, c.Class); err != nil {
		w.deps.Logger.WarnContext(ctx, "starter items not granted; the next join tries again", "character_id", id, "err", err)
	}
	inst, err := w.instance(c.MapID)
	if err != nil {
		return model.View{}, err
	}
	formulas, ok := w.data.Formulas(c.Class)
	if !ok {
		return model.View{}, model.ErrClassUnavailable
	}
	derived := progression.DeriveStats(formulas, c.Stats, c.Level, progression.Equipment{})
	p := model.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats, HP: c.HP, MP: c.MP, X: c.X, Y: c.Y, Connection: conn}
	at := presence{inst: inst, conn: conn}
	prev, had := w.presences.place(id, at)
	err = inst.Join(ctx, p, sink)
	if errors.Is(err, model.ErrBusy) {
		w.presences.undo(id, at, prev, had) // never posted
		return model.View{}, err
	}
	if had && prev.inst != inst {
		prev.inst.LeaveConnection(id, prev.conn)
	}
	if err != nil {
		return model.View{}, err
	}
	return model.View{
		ID: c.ID, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats,
		HP: min(c.HP, derived.MaxHP), MaxHP: derived.MaxHP, MP: min(c.MP, derived.MaxMP), MaxMP: derived.MaxMP,
		MapID: c.MapID, X: c.X, Y: c.Y,
	}, nil
}

// Transfer joins the player to another map on its own goroutine, so the calling tick never waits.
// A presence that a newer join replaced, or whose connection left, is not moved.
func (w *World) Transfer(p model.Player, sink Sink, mapID string) {
	target, ok := w.instances[mapID]
	if !ok {
		w.deps.Logger.Error("transfer to an unknown map", "character_id", p.ID, "map_id", mapID)
		return
	}
	if !w.presences.move(p.ID, p.Connection, target) {
		return
	}
	w.deps.Panics.Go(context.Background(), panics.FlowJob, func() {
		ctx, cancel := context.WithTimeout(context.Background(), transferTimeout)
		defer cancel()
		if err := target.Join(ctx, p, sink); err != nil {
			w.deps.Logger.ErrorContext(ctx, "transfer failed", "character_id", p.ID, "map_id", mapID, "err", err)
		}
	}, "job", "transfer", "character_id", p.ID, "map_id", mapID)
}

// Run ticks every instance, runs the economy workers and saves what the maps queue, until ctx is done.
// The maps then checkpoint the players still on them, and what is left in the queue is saved once a job,
// so a down character service can't hold the shutdown. A map that panics stops them all; the others still
// checkpoint, but its own state is suspect and is not saved.
func (w *World) Run(ctx context.Context) error {
	retrying, stopRetrying := context.WithCancel(ctx)
	defer stopRetrying()
	stopped, saved := make(chan struct{}), make(chan struct{})
	go func() { defer close(saved); w.persistence(retrying, stopped) }()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return w.deps.Workers.Run(gctx) })
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
	stopRetrying()
	close(stopped) // no more jobs can arrive
	<-saved
	return err
}

// persistence is the queue's one consumer: it saves jobs in order, so an older checkpoint never lands
// after a newer one, retrying a transient failure while ctx lasts. Once the maps have stopped it saves
// what is left, once a job.
func (w *World) persistence(ctx context.Context, stopped <-chan struct{}) {
	last := context.WithoutCancel(ctx)
	for {
		select {
		case job := <-w.persist:
			if ctx.Err() == nil && (w.save(ctx, job, saveRetryFor) || ctx.Err() == nil) {
				continue // saved, or given up and logged
			}
			w.save(last, job, 0) // shutting down: one try, also for a job whose retries it cut short
		case <-stopped:
			for {
				select {
				case job := <-w.persist:
					w.save(last, job, 0)
				default:
					return
				}
			}
		}
	}
}

// save saves job, retrying a transient failure for up to retryFor, and reports whether it was saved.
// A failure is logged unless ctx ended, which only cuts the retries short.
func (w *World) save(ctx context.Context, job model.Job, retryFor time.Duration) bool {
	opts := []backoff.RetryOption{
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: saveRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: saveRetryMax}),
		backoff.WithMaxElapsedTime(retryFor),
	}
	if retryFor <= 0 {
		opts = append(opts, backoff.WithMaxTries(1))
	}
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := w.saveOnce(ctx, job)
		if err != nil && !w.deps.Saver.Retryable(err) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	}, opts...)
	if err == nil {
		return true
	}
	if ctx.Err() == nil {
		w.deps.Logger.ErrorContext(ctx, "persistence failed", append(jobAttrs(job), "err", err)...)
	}
	return false
}

// jobAttrs name a job in the log: its kind and its character.
func jobAttrs(job model.Job) []any {
	switch j := job.(type) {
	case model.Checkpoint:
		return []any{"job", "checkpoint", "character_id", j.ID, "map_id", j.MapID}
	case model.LevelUpSave:
		return []any{"job", "level_up", "character_id", j.ID, "level", j.Level}
	}
	return nil
}

func (w *World) saveOnce(ctx context.Context, job model.Job) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	switch j := job.(type) {
	case model.LevelUpSave:
		return w.deps.Saver.SaveLevelUp(ctx, j)
	case model.Checkpoint:
		return w.deps.Saver.SaveCheckpoint(ctx, j)
	}
	return nil
}
