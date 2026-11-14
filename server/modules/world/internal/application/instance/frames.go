package instance

import (
	"slices"

	"finga/internal/kernel"
	"finga/modules/world/internal/model"
)

// broadcast sends the tick's frame and events; it returns the players it dropped for a refused send.
func (i *Instance) broadcast(changed, removed []kernel.CharacterID) []kernel.CharacterID {
	if len(i.order) == 0 {
		i.arena.Unwatched() // an empty map builds no frame, as nobody would get it; a player who joins gets a snapshot
		return nil
	}
	for _, id := range i.order {
		if p := i.players[id]; (p.changed || p.moved) && !slices.Contains(changed, id) {
			changed = append(changed, id)
		}
	}
	views := make([]model.PlayerView, 0, len(changed))
	for _, id := range changed {
		if p, ok := i.players[id]; ok {
			views = append(views, i.view(p))
		}
	}
	mobs, removedMobs := i.arena.Changes()
	drops, removedDrops, collected := i.ground.Changes()
	frame := &model.Frame{Tick: i.tick, Players: views, Mobs: mobs, Drops: drops, Removed: removed, RemovedMobs: removedMobs, RemovedDrops: removedDrops, CollectedDrops: collected}
	refused := i.sendFrame(frame)
	var dropped []kernel.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		ok := !refused[id]
		for _, e := range i.events {
			ok = ok && p.sink.Send(e)
		}
		for _, e := range p.private {
			ok = ok && p.sink.Send(e)
		}
		p.private = p.private[:0]
		p.joined = false
		p.changed = false
		if !ok {
			i.remove(id)
			i.mover.Dropped(i, id, p.Connection)
			dropped = append(dropped, id)
		}
	}
	return dropped
}

// sendFrame sends the frame once per stream the players are on; it returns the players whose stream refused it.
func (i *Instance) sendFrame(frame *model.Frame) map[kernel.CharacterID]bool {
	type group struct {
		stream Stream
		to     []model.Recipient
	}
	var groups []group
	for _, id := range i.order {
		p := i.players[id]
		r := model.Recipient{Character: id, Connection: p.Connection, AckSeq: p.lastSeq}
		if e := p.energyAsOfAck(); e != p.toldEnergy {
			r.Energy, p.toldEnergy = &e, e
		}
		stream := p.sink.Stream()
		k := slices.IndexFunc(groups, func(g group) bool { return g.stream == stream })
		if k < 0 {
			groups = append(groups, group{stream: stream})
			k = len(groups) - 1
		}
		groups[k].to = append(groups[k].to, r)
	}
	refused := map[kernel.CharacterID]bool{}
	for _, g := range groups {
		if !g.stream.SendFrame(frame, g.to) {
			for _, r := range g.to {
				refused[r.Character] = true
			}
		}
	}
	return refused
}
