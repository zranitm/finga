package instance

import (
	"slices"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/services/world/internal/model"
)

// enterPortal reports whether the player left through a portal; an equip in flight settles here first.
func (i *Instance) enterPortal(p *player) bool {
	if p.dead || p.equipping > 0 {
		return false
	}
	for _, portal := range i.m.Portals {
		if !standsIn(portal.Area, p.state.X, p.state.Y) {
			continue
		}
		target, ok := i.data.Map(portal.TargetMap)
		if !ok {
			return false
		}
		k := slices.IndexFunc(target.Portals, func(q gamedata.Portal) bool { return q.Name == portal.TargetPortal })
		if k < 0 {
			return false
		}
		arrival := target.Portals[k].Arrival()
		i.handOff(p, target, arrival.X, arrival.Y)
		return true
	}
	return false
}

// standsIn includes the bottom edge: a character arriving through a portal stands exactly on it.
func standsIn(r sim.Rect, x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

// handOff checkpoints the player on the target map before detaching, so a crash still lands them there.
func (i *Instance) handOff(p *player, target *gamedata.Map, x, y float64) {
	for _, e := range p.private { // this tick's, which the broadcast would have sent
		p.sink.Send(e)
	}
	p.private = nil
	p.sink.Send(model.Teleport{Tick: i.tick, MapID: target.ID, X: x, Y: y})
	moved := p.Player
	moved.X, moved.Y = x, y
	sink := p.sink
	p.state.X, p.state.Y = x, y
	i.checkpointOn(p, target.ID)
	i.tellParty(p, target.ID)
	i.detach(p.ID)
	i.mover.Transfer(i, moved, sink, target.ID)
}
