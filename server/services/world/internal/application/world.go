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
	"finga/pkg/coalesce"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

const (
	busyRetryFirst  = 20 * time.Millisecond // a post to a map whose inbox is full
	busyRetryMax    = 500 * time.Millisecond
	transferTimeout = 5 * time.Second // a transfer's join, retries included
	callTimeout     = 5 * time.Second // one call to another service; past it the call counts as failed
	starterTimeout  = time.Second     // the join's best-effort starter grant

	saveRetryFirst = 100 * time.Millisecond // the pause after a failed save, doubled by each failure in a row
	saveRetryMax   = 5 * time.Second
	flushWorkers   = 8 // the last saves made at once; the queue still hands out one character's at a time
)

// World is the registry of map instances and of where each joined character is, their shared
// persistence worker and transfers.
type World struct {
	data      *gamedata.Data
	deps      WorldDeps
	instances map[string]*Instance
	order     []string
	presences presences
	// saves holds each character's newest checkpoint until the persistence worker has saved it.
	saves *coalesce.Queue[kernel.CharacterID, model.Checkpoint]
	// flushTimeout bounds the last saves on shutdown, all together.
	flushTimeout time.Duration
	// Transfers and take-backs run beside the ticks until Run stops them.
	background     sync.WaitGroup
	jobs           context.Context
	stopBackground context.CancelFunc
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
		saves:        coalesce.New(func(cp model.Checkpoint) kernel.CharacterID { return cp.ID }),
		flushTimeout: t.FlushTimeout,
	}
	w.jobs, w.stopBackground = context.WithCancel(context.Background())
	for _, m := range data.Maps() {
		w.instances[m.ID] = NewInstance(m, data, w.saves, w, t, d.Deps)
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

// Join takes the account's character off the map it is on, loads it with its newest checkpoint not yet
// saved laid over it, grants its starter items and joins its map through conn. So a login elsewhere, or
// a rejoin, starts where the character was, never at an older save; one that fails after the take-off
// detaches the older connection.
func (w *World) Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink Sink) (model.View, error) {
	d, err := w.depart(ctx, id, func(p presence) bool { return p.account == account })
	if err != nil {
		return model.View{}, err
	}
	v, err := w.join(ctx, account, id, conn, sink)
	if err != nil && d.ok {
		d.sink.Send(model.Detached{}) // the gateway joins the older connection again
	}
	return v, err
}

func (w *World) join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink Sink) (model.View, error) {
	unsaved, pending := w.saves.Get(id) // before the load, so a save that lands between them is not missed
	c, err := w.load(ctx, id, account)
	if err != nil {
		return model.View{}, err
	}
	if pending {
		c = withCheckpoint(c, unsaved)
	}
	w.grantStarter(ctx, account, id, c.Class)
	formulas, ok := w.data.Formulas(c.Class)
	if !ok {
		return model.View{}, model.ErrClassUnavailable
	}
	derived := progression.DeriveStats(formulas, c.Stats, c.Level, progression.Equipment{})
	if c.HP <= 0 {
		if c, err = w.respawned(c, derived.MaxHP); err != nil {
			return model.View{}, err
		}
	}
	inst, err := w.instance(c.MapID)
	if err != nil {
		return model.View{}, err
	}
	p := model.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats, HP: c.HP, MP: c.MP, X: c.X, Y: c.Y, Connection: conn}
	at := presence{inst: inst, conn: conn, account: account}
	w.presences.place(id, at)
	if err := inst.Join(ctx, p, sink); err != nil {
		w.presences.forget(id, at)
		if ctx.Err() != nil {
			w.leaveLater(inst, id, conn) // posted, and may still land, though the caller counts it failed
		}
		if errors.Is(err, errSinkClosed) {
			err = model.ErrBusy // the gateway's stream is gone; it joins again on the next one
		}
		return model.View{}, err
	}
	return model.View{
		ID: c.ID, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats,
		HP: min(c.HP, derived.MaxHP), MaxHP: derived.MaxHP, MP: min(c.MP, derived.MaxMP), MaxMP: derived.MaxMP,
		MapID: c.MapID, X: c.X, Y: c.Y,
	}, nil
}

func (w *World) load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.Characters.Load(ctx, id, account)
}

// grantStarter is a safety net for a creation whose grant failed: a failure is logged, and the next join
// tries again.
func (w *World) grantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) {
	ctx, cancel := context.WithTimeout(ctx, starterTimeout)
	defer cancel()
	if err := w.deps.Starter.GrantStarter(ctx, account, id, class); err != nil {
		w.deps.Logger.WarnContext(ctx, "starter items not granted; the next join tries again", "character_id", id, "err", err)
	}
}

// departure is a player that a leave took off its map.
type departure struct {
	sink Sink
	ok   bool // the player was on the map
}

// depart takes the character off its map, if want accepts its presence, and returns once the map has, its
// last checkpoint queued. Until then a tombstone stands in the registry: a transfer can't land, and
// another depart waits for this one before it looks again.
func (w *World) depart(ctx context.Context, id kernel.CharacterID, want func(presence) bool) (departure, error) {
	for {
		left := make(chan struct{})
		p, ok := w.presences.depart(id, want, left)
		if !ok {
			return departure{}, nil
		}
		if p.left != nil {
			if err := p.inst.wait(ctx, p.left); err != nil {
				return departure{}, err
			}
			w.presences.forget(id, p)
			continue
		}
		tomb := p
		tomb.left = left
		var d departure
		if err := postBusy(ctx, p.inst, leaveMsg{id: id, conn: p.conn, done: left, took: &d}); err != nil {
			w.presences.restore(id, tomb, p)
			close(left) // never posted; whoever waits on the tombstone looks again
			return departure{}, err
		}
		if err := p.inst.wait(ctx, left); err != nil {
			return departure{}, err // the leave lands later, and its tombstone stands until then
		}
		w.presences.forget(id, tomb)
		return d, nil
	}
}

// leaveLater posts a leave for a join that may still land, retrying while the map is busy, until it is
// posted or the world stops.
func (w *World) leaveLater(inst *Instance, id kernel.CharacterID, conn string) {
	w.background.Go(func() { _ = postBusy(w.jobs, inst, leaveMsg{id: id, conn: conn}) })
}

// postBusy posts m to inst, retrying while its inbox is full, until ctx ends or the map stops.
func postBusy(ctx context.Context, inst *Instance, m message) error {
	return whileBusy(ctx, func() error { return inst.post(m) })
}

// whileBusy runs try until it answers anything but ErrBusy, backing off between tries, or until ctx ends.
func whileBusy(ctx context.Context, try func() error) error {
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := try()
		if err != nil && !errors.Is(err, model.ErrBusy) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	}, backoff.WithBackOff(busyBackOff()), backoff.WithMaxElapsedTime(0))
	return err
}

// respawned is c, saved dead before dead players were saved as respawned, as the respawn it was owed.
func (w *World) respawned(c model.Character, maxHP int) (model.Character, error) {
	m, ok := w.data.Map(c.MapID)
	if !ok {
		return c, model.ErrNoMap
	}
	town, _ := w.data.Map(m.Town)
	c.MapID, c.X, c.Y, c.HP = town.ID, town.PlayerSpawn.X, town.PlayerSpawn.Y, respawnHP(maxHP)
	return c, nil
}

// withCheckpoint is c as cp left it.
func withCheckpoint(c model.Character, cp model.Checkpoint) model.Character {
	c.MapID, c.X, c.Y, c.HP, c.MP, c.Level, c.Exp, c.Stats = cp.MapID, cp.X, cp.Y, cp.HP, cp.MP, cp.Level, cp.Exp, cp.Stats
	return c
}

func busyBackOff() *backoff.ExponentialBackOff {
	return &backoff.ExponentialBackOff{InitialInterval: busyRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: busyRetryMax}
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
	w.background.Go(func() {
		w.deps.Panics.Do(w.jobs, panics.FlowJob, func() { w.transfer(p, sink, target) }, "job", "transfer", "character_id", p.ID, "map_id", mapID)
	})
}

// Dropped forgets the presence of a player its map removed for a failed sink, so the registry holds only players on a map.
func (w *World) Dropped(from *Instance, id kernel.CharacterID, conn string) {
	w.presences.drop(id, from, conn)
}

// errMoved is a transfer whose presence left, or was replaced, before it landed.
var errMoved = errors.New("the presence moved on")

// transfer joins the player to target, retrying while its inbox is full. One that cannot land within
// transferTimeout detaches the connection, so the gateway joins the player again where it was saved.
func (w *World) transfer(p model.Player, sink Sink, target *Instance) {
	ctx, cancel := context.WithTimeout(w.jobs, transferTimeout)
	defer cancel()
	at := presence{inst: target, conn: p.Connection, account: p.Account}
	held := func() bool { return w.presences.holds(p.ID, at) }
	err := whileBusy(ctx, func() error {
		if !held() {
			return errMoved
		}
		return target.join(ctx, &joinMsg{player: p, sink: sink, held: held})
	})
	switch {
	case errors.Is(err, errMoved):
	case err == nil:
		if !held() {
			w.leaveLater(target, p.ID, p.Connection) // it left while the join landed
		}
	case w.jobs.Err() != nil:
		// Shutting down: the player was saved on target, and the gateway joins them again after the restart.
	default:
		w.leaveLater(target, p.ID, p.Connection) // a join posted as time ran out may still land
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
	w.stopBackground()
	w.background.Wait()
	stopRetrying()
	close(stopped) // no more jobs can arrive
	<-saved
	return err
}

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

// saveWhile saves the queue's checkpoints until ctx ends. A save that fails transiently goes back in line
// behind the others, and failures in a row back the worker off, up to saveRetryMax: one failing save holds
// up no other, and an outage costs every save the same wait. A refused save is dropped and logged.
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
