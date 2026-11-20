// Package world runs the maps together: who is joined where, the commands routed to them, transfers and saves.
package world

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/coalesce"
	"finga/pkg/keylock"
	"finga/pkg/panics"
)

const (
	busyRetryFirst  = 20 * time.Millisecond // a post to a map whose inbox is full
	busyRetryMax    = 500 * time.Millisecond
	transferTimeout = 5 * time.Second // a transfer's join, retries included
	answerTimeout   = 5 * time.Second // a map's answer to a join or leave, waited for past the caller's deadline
	callTimeout     = 5 * time.Second // one call to another module; past it the call counts as failed
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
	opened    opened // matches and channels, opened on demand
	fighting  fighting
	channels  channels
	tuning    instance.Tuning
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
	// mapsCtx is done once Run has stopped the maps and their transfers: no checkpoint comes after it.
	mapsCtx     context.Context
	mapsStopped context.CancelFunc
}

// Deps are what the World calls besides its maps'; all are required.
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
	Channels     ChannelObserver
	Results      instance.Results // where matches' outcomes go
}

// Tuning is how often the maps tick and checkpoint, how long the last saves may take on shutdown, how many drops one grant call may carry, and how a region's channels fill and close.
type Tuning struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	FlushTimeout       time.Duration
	BatchMax           int
	ChannelCap         int           // players a region's channel takes before a pick passes it
	ChannelIdle        time.Duration // how long a channel past the first stays open empty
}

// New builds an instance for every map in d.Data; the World moves their players between maps.
func New(d Deps, t Tuning) *World {
	data := d.Data
	w := &World{
		data: data, deps: d, instances: map[string]*instance.Instance{}, presences: newPresences(), locks: keylock.New[kernel.CharacterID](),
		saves:        coalesce.New(func(cp model.Checkpoint) kernel.CharacterID { return cp.ID }),
		flushTimeout: t.FlushTimeout, tick: t.TickInterval,
		channels: channels{cap: t.ChannelCap, idle: t.ChannelIdle, regions: map[string]*region{}, observer: d.Channels},
	}
	w.mapsCtx, w.mapsStopped = context.WithCancel(context.Background())
	w.jobsCtx, w.stopBackground = context.WithCancel(context.Background())
	w.tuning = instance.Tuning{TickInterval: t.TickInterval, CheckpointInterval: t.CheckpointInterval, BatchMax: t.BatchMax}
	for _, m := range data.Maps() {
		m.Grid() // built now, a match's map's too, so no tick waits on one
	}
	for _, m := range data.Maps() {
		if m.Kind == gamedata.Pvp {
			continue // a match opens its own
		}
		deps := instance.Deps{Services: d.Services, Map: m, Data: data, Saves: w.saves, Mover: w, Logger: d.Logger}
		if m.Kind == gamedata.Region {
			deps.Channel = 1
		}
		w.instances[m.ID] = instance.New(deps, w.tuning)
		w.order = append(w.order, m.ID)
		if m.Kind == gamedata.Region {
			w.channels.regions[m.ID] = &region{m: m, open: []*channel{{n: 1, inst: w.instances[m.ID], id: m.ID}}}
			d.Channels.ChannelsOpen(m.ID, 1)
		}
	}
	return w
}

// mapNamed is the instance character id joins on map mapID, and what to call once they've joined or failed: in a region, the channel picked for them.
func (w *World) mapNamed(id kernel.CharacterID, mapID string) (*instance.Instance, func(), error) {
	if r, ok := w.channels.regions[mapID]; ok {
		return w.pickChannel(id, r, 0, false)
	}
	i, ok := w.instances[mapID]
	if !ok {
		return nil, nil, model.ErrNoMap
	}
	return i, func() {}, nil
}

// Run ticks every map, and every match as it opens, until ctx is done or one panics, whose suspect state is not saved; then it stops the transfers, and Save flushes the rest. Run once.
func (w *World) Run(ctx context.Context) error {
	defer w.mapsStopped() // no more checkpoints can arrive
	runCtx, fail := context.WithCancelCause(ctx)
	defer fail(nil)
	w.opened.start(runCtx, fail)
	if len(w.channels.regions) > 0 {
		w.background.Go(w.closeIdleChannels)
	}
	g, gctx := errgroup.WithContext(runCtx)
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
	fail(nil) // the matches and channels stop with the maps
	err = cmp.Or(err, w.opened.stop())
	w.stopBackground()
	w.background.Wait()
	return err
}

// Save retries the maps' checkpoints until Run has stopped them, then each one left until it lands or the flush budget ends; it outlasts ctx, as the last checkpoints come after.
func (w *World) Save(context.Context) error {
	return w.persistence()
}
