package world

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
	"finga/pkg/transient"
)

// matches are the instances the World opens on demand and that close themselves, each on its own goroutine under the World's run: a match's now.
type matches struct {
	mu   sync.Mutex
	by   map[string]*instance.Instance
	ctx  context.Context // Run's while it runs: nil before, and done after
	fail context.CancelCauseFunc
	runs sync.WaitGroup
	err  error // the first failure, a panic, which stops the World as a map's does
}

// start lets instances open under ctx until it's done; fail stops the World.
func (ms *matches) start(ctx context.Context, fail context.CancelCauseFunc) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.by, ms.ctx, ms.fail = map[string]*instance.Instance{}, ctx, fail
}

// run starts inst under id; ErrStopped when the World isn't running.
func (ms *matches) run(id string, inst *instance.Instance) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.ctx == nil || ms.ctx.Err() != nil {
		return model.ErrStopped
	}
	if _, dup := ms.by[id]; dup {
		return fmt.Errorf("world: instance %s is open already", id)
	}
	ms.by[id] = inst
	ctx := ms.ctx
	ms.runs.Go(func() {
		err := func() (err error) {
			defer panics.Capture(&err)
			return inst.Run(ctx)
		}()
		ms.mu.Lock()
		defer ms.mu.Unlock()
		delete(ms.by, id)
		if err != nil && ms.err == nil {
			ms.err = fmt.Errorf("instance %s: %w", id, err)
			ms.fail(ms.err)
		}
	})
	return nil
}

// get is the open instance id.
func (ms *matches) get(id string) (*instance.Instance, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	inst, ok := ms.by[id]
	return inst, ok
}

// stop opens no more, and waits for those open to stop; it returns the first failure.
func (ms *matches) stop() error {
	ms.mu.Lock()
	ms.ctx = nil
	ms.mu.Unlock()
	ms.runs.Wait()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.err
}

// instanceAt is a map's instance or an open match's, by id.
func (w *World) instanceAt(id string) (*instance.Instance, bool) {
	if inst, ok := w.instances[id]; ok {
		return inst, true
	}
	return w.matches.get(id)
}

// OpenMatch opens spec's match on its mode's map and hands each of its characters to it from where they are; one who can't go counts as gone once the match's prep ends.
func (w *World) OpenMatch(ctx context.Context, spec model.MatchSpec) error {
	mode, ok := w.data.PvpMode(spec.Mode)
	if !ok {
		return model.ErrNoMap
	}
	m, _ := w.data.Map(mode.Map)
	id := spec.ID.String()
	inst := instance.New(instance.Deps{
		Services: w.deps.Services, Map: m, Data: w.data, Saves: w.saves, Mover: w, Logger: w.deps.Logger,
		Match: &instance.Match{Spec: spec, Mode: mode, Results: w.deps.Results},
	}, w.tuning)
	if err := w.matches.run(id, inst); err != nil {
		return err
	}
	for s, side := range spec.Sides {
		for k, f := range side {
			if f.Bot != nil {
				continue
			}
			err := w.toMatch(ctx, f.Character, id, m.ID, s, k)
			if err != nil {
				w.deps.Logger.WarnContext(ctx, "a fighter didn't go into their match", "character_id", f.Character, "match_id", id, "err", err)
			}
		}
	}
	return nil
}

// toMatch has the character's map hand them to the match, at spot k of side s.
func (w *World) toMatch(ctx context.Context, id kernel.CharacterID, matchID, mapID string, s, k int) error {
	at, ok := w.presences.get(id)
	if !ok || at.transit {
		return fmt.Errorf("%w: the character is on no map", transient.ErrUnavailable)
	}
	m, _ := w.data.Map(mapID)
	return whileBusy(ctx, func() error { return at.inst.ToMatch(ctx, id, matchID, mapID, m.Sides[s][k]) })
}

// CanFight is nil when the character could go into a match now: on a map, alive, promoted, and in no match.
func (w *World) CanFight(ctx context.Context, id kernel.CharacterID) error {
	at, ok := w.presences.get(id)
	switch {
	case !ok || at.transit:
		return fmt.Errorf("%w: the character is on no map", transient.ErrUnavailable)
	case at.inst.IsMatch():
		return model.ErrInMatch
	}
	err := at.inst.CanFight(ctx, id)
	if errors.Is(err, model.ErrBusy) {
		return fmt.Errorf("%w: the map's inbox is full", transient.ErrUnavailable)
	}
	return err
}

// Forfeit takes the character out of their match, back where they came from.
func (w *World) Forfeit(ctx context.Context, id kernel.CharacterID, conn string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Forfeit(ctx, id, conn) })
}
