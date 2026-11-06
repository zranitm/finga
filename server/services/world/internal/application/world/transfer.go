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
	if !w.presences.move(p.ID, p.Connection, target) {
		return
	}
	w.background.Go(func() {
		w.deps.Panics.Do(w.jobs, panics.FlowJob, func() { w.transfer(p, sink, target) }, "job", "transfer", "character_id", p.ID, "map_id", mapID)
	})
}

// Dropped forgets the presence of a player its map removed for a failed sink, so the registry holds only players on a map.
func (w *World) Dropped(from *instance.Instance, id kernel.CharacterID, conn string) {
	w.presences.drop(id, from, conn)
}

// transfer joins the player to target; one that can't land in transferTimeout detaches, so the gateway joins it again.
func (w *World) transfer(p model.Player, sink instance.Sink, target *instance.Instance) {
	ctx, cancel := context.WithTimeout(w.jobs, transferTimeout)
	defer cancel()
	at := presence{inst: target, conn: p.Connection, account: p.Account}
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
	case w.jobs.Err() != nil:
		// Shutting down: the player was saved on target, and the gateway joins them again after the restart.
	default:
		w.leaveLater(target, p.ID, p.Connection) // a join posted as time ran out may still land
		if w.presences.forget(p.ID, at) {
			w.deps.Logger.ErrorContext(ctx, "transfer failed; the gateway joins the player again", "character_id", p.ID, "map_id", target.MapID(), "err", err)
			sink.Send(model.Detached{})
		}
	}
}
