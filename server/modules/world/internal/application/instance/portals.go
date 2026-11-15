package instance

import (
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/modules/world/internal/model"
)

// enterPortal reports whether the player left through a portal; an item call in flight settles here first.
func (i *Instance) enterPortal(p *player) bool {
	if p.dead || p.itemCalls > 0 {
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
		i.handOff(p, target, arrival.X, arrival.Y, p.state.Facing)
		return true
	}
	return false
}

// standsIn includes the bottom edge: a character arriving through a portal stands exactly on it.
func standsIn(r sim.Rect, x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

// handOff checkpoints the player on the target map before detaching, so a crash still lands them there; it arrives facing a side.
func (i *Instance) handOff(p *player, target *gamedata.Map, x, y float64, facing int) {
	i.tellQuestsOf(p)
	for _, e := range p.private { // this tick's, which the broadcast would have sent
		p.sink.Send(e)
	}
	p.private = nil
	p.sink.Send(model.Teleport{Tick: i.tick, MapID: target.ID, X: x, Y: y, Yaw: model.YawOf(facing)})
	moved := p.Player
	moved.X, moved.Y, moved.Yaw = x, y, model.YawOf(facing)
	moved.Timers = i.timersOf(p, time.Now())
	sink := p.sink
	p.state.X, p.state.Y, p.state.Facing = x, y, facing
	i.checkpointOn(p, target.ID)
	i.tellParty(p, target.ID)
	i.detach(p.ID)
	i.mover.Transfer(i, moved, sink, target.ID)
}
