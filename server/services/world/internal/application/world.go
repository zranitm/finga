// Package application runs the map instances: one actor per map, ticking at the configured rate.
// Services are called from worker goroutines, never inside a tick.
package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	persistQueue       = 4096
	transferTimeout    = 5 * time.Second // a transfer's join, retries included
	transferRetryFirst = 20 * time.Millisecond
	transferRetryMax   = 500 * time.Millisecond
	callTimeout        = 5 * time.Second // one call to another service; past it the call counts as failed

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
	// flushTimeout bounds the last saves on shutdown, all together.
	flushTimeout time.Duration
	// Transfers run beside the ticks until Run stops them.
	transfers     sync.WaitGroup
	jobs          context.Context
	stopTransfers context.CancelFunc
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
		persist: make(chan model.Job, persistQueue), flushTimeout: t.FlushTimeout,
	}
	w.jobs, w.stopTransfers = context.WithCancel(context.Background())
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
		if ctx.Err() != nil {
			inst.LeaveConnection(id, conn) // posted, and may still land, though the caller counts it failed
		}
		w.presences.forget(id, at)
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
	w.transfers.Go(func() {
		w.deps.Panics.Do(w.jobs, panics.FlowJob, func() { w.transfer(p, sink, target) }, "job", "transfer", "character_id", p.ID, "map_id", mapID)
	})
}

// errMoved is a transfer whose presence left, or was replaced, before it landed.
var errMoved = errors.New("the presence moved on")

// transfer joins the player to target, retrying while its inbox is full. One that cannot land within
// transferTimeout detaches the connection, so the gateway joins the player again where it was saved.
func (w *World) transfer(p model.Player, sink Sink, target *Instance) {
	ctx, cancel := context.WithTimeout(w.jobs, transferTimeout)
	defer cancel()
	at := presence{inst: target, conn: p.Connection}
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		if !w.presences.holds(p.ID, at) {
			return struct{}{}, backoff.Permanent(errMoved)
		}
		err := target.join(ctx, &joinMsg{player: p, sink: sink, transfer: true})
		if errors.Is(err, errReplaced) {
			return struct{}{}, backoff.Permanent(errMoved)
		}
		if err != nil && !errors.Is(err, model.ErrBusy) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: transferRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: transferRetryMax}),
		backoff.WithMaxElapsedTime(0),
	)
	switch {
	case errors.Is(err, errMoved):
	case err == nil:
		if !w.presences.holds(p.ID, at) {
			target.LeaveConnection(p.ID, p.Connection) // it left while the join landed
		}
	case w.jobs.Err() != nil:
		// Shutting down: the player was saved on target, and the gateway joins them again after the restart.
	default:
		target.LeaveConnection(p.ID, p.Connection) // a join posted as time ran out may still land
		if w.presences.forget(p.ID, at) {
			w.deps.Logger.ErrorContext(ctx, "transfer failed; the gateway joins the player again", "character_id", p.ID, "map_id", target.m.ID, "err", err)
			sink.Send(model.Detached{})
		}
	}
}

// Run ticks every instance, runs the economy workers and saves what the maps queue, until ctx is done.
// The maps then checkpoint the players still on them, and the saves left get one try each within the
// flush budget, so a down character service can't hold the shutdown. A map that panics stops them all;
// the others still checkpoint, but its own state is suspect and is not saved.
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
	w.stopTransfers()
	w.transfers.Wait()
	stopRetrying()
	close(stopped) // no more jobs can arrive
	<-saved
	return err
}

// persistence is the queue's one consumer: it saves jobs in order, so an older checkpoint never lands
// after a newer one, retrying a transient failure while ctx lasts. Then each job left, the one whose
// retries ctx cut short first, gets one try within the flush budget, until the maps have stopped.
func (w *World) persistence(ctx context.Context, stopped <-chan struct{}) {
	cut := w.saveUntilDone(ctx)
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.flushTimeout)
	defer cancel()
	if cut != nil {
		w.lastTry(final, cut)
	}
	for {
		select {
		case job := <-w.persist:
			w.lastTry(final, job)
		case <-stopped:
			for {
				select {
				case job := <-w.persist:
					w.lastTry(final, job)
				default:
					return
				}
			}
		}
	}
}

// saveUntilDone saves the queue's jobs until ctx ends, and returns the one whose retries it cut short.
func (w *World) saveUntilDone(ctx context.Context) model.Job {
	for {
		select {
		case job := <-w.persist:
			if !w.save(ctx, job) && ctx.Err() != nil {
				return job
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// save saves job, retrying a transient failure for up to saveRetryFor, and reports whether it was saved.
// A failure is logged unless ctx ended, which only cuts the retries short.
func (w *World) save(ctx context.Context, job model.Job) bool {
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := w.saveOnce(ctx, job)
		if err != nil && !w.deps.Saver.Retryable(err) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: saveRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: saveRetryMax}),
		backoff.WithMaxElapsedTime(saveRetryFor),
	)
	if err == nil {
		return true
	}
	if ctx.Err() == nil {
		w.deps.Logger.ErrorContext(ctx, "persistence failed", append(jobAttrs(job), "err", err)...)
	}
	return false
}

// lastTry saves job once on shutdown; a failure is logged, as the save is lost.
func (w *World) lastTry(ctx context.Context, job model.Job) {
	if err := w.saveOnce(ctx, job); err != nil {
		w.deps.Logger.ErrorContext(ctx, "persistence failed", append(jobAttrs(job), "shutdown", true, "err", err)...)
	}
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
