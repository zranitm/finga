package instance

import (
	"slices"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// broadcast sends the tick's frame and events, dropping a player whose stream refuses them.
func (i *Instance) broadcast(changed []kernel.CharacterID) {
	if len(i.order) == 0 {
		i.arena.Unwatched() // an empty map builds no frame, as nobody would get it; a player who joins gets a snapshot
		i.interest.rest()
		return
	}
	i.stampFights()
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
	own := ownViews(views)
	fullViews, steps := i.stepPlayers(views)
	fullMobs, mobSteps := i.stepMobs(mobs, removedMobs)
	frame := i.cut(fullViews, steps, fullMobs, mobSteps, removedMobs)
	refused := i.sendFrame(frame, own)
	heard := i.heard()
	var dropped []kernel.CharacterID
	for _, id := range i.order {
		p := i.players[id]
		ok := !refused[id]
		at := i.interest.watches[id].cell
		for k, e := range i.events {
			if heard[k].by(i.interest, at) {
				ok = ok && p.sink.Send(e)
			}
		}
		for _, e := range p.private {
			ok = ok && p.sink.Send(e)
		}
		p.private = p.private[:0]
		p.joined = false
		p.changed = false
		if !ok {
			dropped = append(dropped, id)
		}
	}
	// Removed once every player has the tick's events: what a removal raises, a match's stand-in or end, goes out next tick.
	sent := len(i.events)
	for _, id := range dropped {
		conn := i.players[id].Connection
		i.remove(id)
		i.mover.Dropped(i, id, conn)
	}
	i.later = append(i.later, i.events[sent:]...)
}

// sendFrame sends the frame once per stream the players are on, each player their block's parts; it returns the players whose stream refused it.
func (i *Instance) sendFrame(frame *model.Frame, own map[kernel.CharacterID]model.PlayerView) map[kernel.CharacterID]bool {
	type group struct {
		stream Stream
		to     []model.Recipient
	}
	var groups []group
	all := make([]int, 0, 4*len(i.order)) // every player's parts, a few each
	for _, id := range i.order {
		p := i.players[id]
		parts, unseen := i.follow(frame, id, &all)
		r := model.Recipient{Character: id, Connection: p.Connection, Parts: parts, AckSeq: p.lastSeq, Unseen: unseen, Pouches: i.ground.Changes(id)}
		if v, ok := own[id]; ok && !framedWhole(frame, parts, id) {
			r.Own = &v
		}
		if e := p.energyAsOfAck(); e != p.toldEnergy {
			r.Energy, p.toldEnergy = &e, e
		}
		stream := p.sink.Stream()
		k := slices.IndexFunc(groups, func(g group) bool { return g.stream == stream })
		if k < 0 {
			groups = append(groups, group{stream: stream, to: make([]model.Recipient, 0, len(i.order))})
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

// framedWhole is whether the player's own view went whole in one of the parts they are sent, so their private part need not carry it again.
func framedWhole(frame *model.Frame, parts []int, id kernel.CharacterID) bool {
	for _, k := range parts {
		for _, v := range frame.Parts[k].Players {
			if v.ID == id {
				return true
			}
		}
	}
	return false
}
