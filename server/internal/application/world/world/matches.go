package world

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// instanceAt is a map's instance or an open match's, by id.
func (w *World) instanceAt(id string) (*instance.Instance, bool) {
	if inst, ok := w.instances[id]; ok {
		return inst, true
	}
	return w.opened.get(id)
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
	var players []kernel.CharacterID
	for _, side := range spec.Sides {
		for _, f := range side {
			if f.Bot == nil {
				players = append(players, f.Character)
			}
		}
	}
	w.fighting.enter(id, players) // before it returns, so a build saved from then on finds the match
	if err := w.opened.run(id, inst, func() { w.fighting.leave(id, players) }); err != nil {
		w.fighting.leave(id, players)
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

// SetBuild hands the character's match their arena build; nothing when they're in none.
func (w *World) SetBuild(ctx context.Context, id kernel.CharacterID, b model.Build) error {
	matchID, ok := w.fighting.of(id)
	if !ok {
		return nil
	}
	inst, ok := w.opened.get(matchID)
	if !ok {
		return nil
	}
	return whileBusy(ctx, func() error { return inst.SetBuild(ctx, id, b) })
}

// fighting is the open match each player is in, from its opening until it has stopped.
type fighting struct {
	mu sync.Mutex
	in map[kernel.CharacterID]string
}

// enter has players in match id.
func (f *fighting) enter(id string, players []kernel.CharacterID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.in == nil {
		f.in = map[kernel.CharacterID]string{}
	}
	for _, p := range players {
		f.in[p] = id
	}
}

// leave has players out of match id, those a later match hasn't taken.
func (f *fighting) leave(id string, players []kernel.CharacterID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range players {
		if f.in[p] == id {
			delete(f.in, p)
		}
	}
}

// of is the match the player is in.
func (f *fighting) of(p kernel.CharacterID) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.in[p]
	return id, ok
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
