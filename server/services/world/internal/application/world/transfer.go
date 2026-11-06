package world

import (
	"context"
	"errors"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

// Transfer joins the player to another map on its own goroutine, so the calling tick never waits.
func (w *World) Transfer(p model.Player, sink instance.Sink, mapID string) {
	target, ok := w.instances[mapID]
	if !ok {
		w.deps.Logger.Error("transfer to an unknown map", "character_id", p.ID, "map_id", mapID)
		return
	}
	at, ok := w.presences.move(p.ID, p.Connection, target)
	if !ok {
		return
	}
	w.background.Go(func() {
		w.deps.Panics.Do(w.jobs, panics.FlowJob, func() { w.transfer(p, sink, at) }, "job", "transfer", "character_id", p.ID, "map_id", mapID)
	})
}

// Dropped forgets the presence of a player its map removed for a failed sink, so the registry holds only players on a map.
func (w *World) Dropped(from *instance.Instance, id kernel.CharacterID, conn string) {
	w.presences.drop(id, from, conn)
}

// transfer joins the player to the map at names; one that can't land in transferTimeout detaches, so the gateway joins it again.
func (w *World) transfer(p model.Player, sink instance.Sink, at presence) {
	ctx, cancel := context.WithTimeout(w.jobs, transferTimeout)
	defer cancel()
	target := at.inst
	held := func() bool { return w.presences.holds(p.ID, at) }
	err := whileBusy(ctx, func() error {
		if !held() {
			return instance.ErrMoved
		}
		return target.Arrive(ctx, p, sink, held)
	})
	switch {
	case errors.Is(err, instance.ErrMoved):
	case err == nil:
		if !held() {
			w.leaveLater(target, p.ID, p.Connection) // it left while the join landed
		}
	case w.jobs.Err() != nil, errors.Is(err, model.ErrStopped):
		// Shutting down, maps stopping one by one: the player was saved on target, and the gateway joins them again after the restart.
	default:
		w.leaveLater(target, p.ID, p.Connection) // a join posted as time ran out may still land
		if w.presences.forget(p.ID, at) {
			w.deps.Logger.ErrorContext(ctx, "transfer failed; the gateway joins the player again", "character_id", p.ID, "map_id", target.MapID(), "err", err)
			sink.Send(model.Detached{})
		}
	}
}
