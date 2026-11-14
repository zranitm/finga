package world

import (
	"context"
	"errors"

	"finga/internal/kernel"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/pkg/panics"
)

// Transfer marks the player in transit from the calling map and lands them on another on its own goroutine, so the calling tick never waits nor takes the character's lock.
func (w *World) Transfer(from *instance.Instance, p model.Player, sink instance.Sink, mapID string) {
	target, ok := w.instances[mapID]
	if !ok {
		w.deps.Logger.Error("transfer to an unknown map", "character_id", p.ID, "map_id", mapID)
		return
	}
	at, ok := w.presences.handOff(p.ID, p.Connection, from, target)
	if !ok {
		return // a newer join took the presence, or a leave took it off
	}
	w.background.Go(func() {
		w.deps.Panics.Do(w.jobsCtx, panics.FlowJob, func() { w.land(p, sink, at) }, "job", "transfer", "character_id", p.ID, "map_id", mapID)
	})
}

// Dropped forgets the presence of a player its map removed for a failed sink, so the registry holds only players on a map.
func (w *World) Dropped(from *instance.Instance, id kernel.CharacterID, conn string) {
	w.presences.drop(id, from, conn)
}

// errReplaced is a landing whose presence was taken off or replaced while it waited for the character's lock.
var errReplaced = errors.New("world: the presence moved on")

// land joins a handed-off player to the map at names; one that can't land in transferTimeout detaches, so the gateway joins it again.
func (w *World) land(p model.Player, sink instance.Sink, at presence) {
	ctx, cancel := context.WithTimeout(w.jobsCtx, transferTimeout)
	defer cancel()
	err := whileBusy(ctx, func() error { return w.arrive(ctx, p, sink, at) })
	switch {
	case err == nil, errors.Is(err, errReplaced):
	case w.jobsCtx.Err() != nil, errors.Is(err, model.ErrStopped):
		// Shutting down, maps stopping one by one: the player was saved on target, and the gateway joins them again after the restart.
	default:
		if w.presences.forget(p.ID, at.gen) {
			w.deps.Logger.ErrorContext(ctx, "transfer failed; the gateway joins the player again", "character_id", p.ID, "map_id", at.inst.MapID(), "err", err)
			sink.Send(model.Detached{})
		}
	}
}

// arrive is one try at landing, under the character's lock, which is let go between tries so a join or leave never waits on a busy map.
func (w *World) arrive(ctx context.Context, p model.Player, sink instance.Sink, at presence) error {
	unlock, err := w.locks.Lock(ctx, p.ID)
	if err != nil {
		return err
	}
	defer unlock()
	if !w.presences.holds(p.ID, at.gen) {
		return errReplaced
	}
	err = at.inst.Join(ctx, p, sink)
	switch {
	case err == nil:
		w.presences.landed(p.ID, at.gen)
	case ctx.Err() != nil && !errors.Is(err, model.ErrBusy):
		w.leaveBehind(at.inst, p.ID, p.Connection) // posted, and the map has not answered: it may still land
	}
	return err
}
