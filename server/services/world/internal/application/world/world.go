// Package world runs the maps together: who is joined where, the commands routed to them, transfers and saves.
package world

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/coalesce"
	"finga/pkg/keylock"
	"finga/pkg/panics"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

const (
	busyRetryFirst  = 20 * time.Millisecond // a post to a map whose inbox is full
	busyRetryMax    = 500 * time.Millisecond
	transferTimeout = 5 * time.Second // a transfer's join, retries included
	answerTimeout   = 5 * time.Second // a map's answer to a join or leave, waited for past the caller's deadline
	callTimeout     = 5 * time.Second // one call to another service; past it the call counts as failed
	starterTimeout  = time.Second     // the join's best-effort starter grant

	saveRetryFirst = 100 * time.Millisecond // the pause after a failed save, doubled by each failure in a row
	saveRetryMax   = 5 * time.Second
	unexplainedFor = time.Minute // a save failing without a reason is retried this long, past a passing fault, then dropped
	flushWorkers   = 8           // the last saves made at once; the queue still hands out one character's at a time
)

// World is the registry of map instances and of where each joined character is; it runs their saves and transfers.
type World struct {
	data      *gamedata.Data
	deps      Deps
	instances map[string]*instance.Instance
	order     []string
	presences presences
	locks     *keylock.Locks[kernel.CharacterID] // every presence change of a character but the tick's, one at a time
	// saves holds each character's newest checkpoint until the persistence worker has saved it.
	saves *coalesce.Queue[kernel.CharacterID, model.Checkpoint]
	// flushTimeout bounds the last saves on shutdown, all together.
	flushTimeout time.Duration
	tick         time.Duration // every map's
	// Transfers run beside the ticks until Run stops them.
	background     sync.WaitGroup
	jobsCtx        context.Context
	stopBackground context.CancelFunc
}

// Deps are the services the World calls besides its maps'; all are required.
type Deps struct {
	instance.Services
	Data         *gamedata.Data
	Saver        Saver
	Characters   Characters
	Starter      Starter
	Wardrobe     Wardrobe
	HandedIns    HandedIns
	Panics       *panics.Recorder // a transfer's panic
	Logger       *slog.Logger
	SaveObserver SaveObserver
}

// Tuning is how often the maps tick and checkpoint, how long the last saves may take on shutdown, and how many drops one grant call may carry.
type Tuning struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	FlushTimeout       time.Duration
	BatchMax           int
}

// New builds an instance for every map in d.Data; the World moves their players between maps.
func New(d Deps, t Tuning) *World {
	data := d.Data
	w := &World{
		data: data, deps: d, instances: map[string]*instance.Instance{}, presences: newPresences(), locks: keylock.New[kernel.CharacterID](),
		saves:        coalesce.New(func(cp model.Checkpoint) kernel.CharacterID { return cp.ID }),
		flushTimeout: t.FlushTimeout, tick: t.TickInterval,
	}
	w.jobsCtx, w.stopBackground = context.WithCancel(context.Background())
	for _, m := range data.Maps() {
		w.instances[m.ID] = instance.New(instance.Deps{Services: d.Services, Map: m, Data: data, Saves: w.saves, Mover: w, Logger: d.Logger}, instance.Tuning{TickInterval: t.TickInterval, CheckpointInterval: t.CheckpointInterval, BatchMax: t.BatchMax})
		w.order = append(w.order, m.ID)
	}
	return w
}

// mapNamed finds a map's actor.
func (w *World) mapNamed(mapID string) (*instance.Instance, error) {
	i, ok := w.instances[mapID]
	if !ok {
		return nil, model.ErrNoMap
	}
	return i, nil
}

// Run ticks every instance and saves until ctx is done or a map panics; a panicked map's suspect state is not saved.
func (w *World) Run(ctx context.Context) error {
	retryCtx, stopRetrying := context.WithCancel(ctx)
	defer stopRetrying()
	stopped, saved := make(chan struct{}), make(chan struct{})
	go func() { defer close(saved); w.persistence(retryCtx, stopped) }()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return w.deps.ItemCalls.Run(gctx) })
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
