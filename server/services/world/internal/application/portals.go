package application

import (
	"slices"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/services/world/internal/model"
)

// enterPortal moves a player standing in one of this map's portals to the
// linked portal on the target map (page 3: leave, checkpoint, join the
// target, MapChange). It reports whether the player left this instance.
// Off a portal, dead, or with no mover, nothing happens.
func (i *Instance) enterPortal(p *player) bool {
	if p.dead || i.opts.Transfer == nil {
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

// standsIn reports whether feet at (x, y) are inside a portal rectangle.
// The bottom edge counts: a character arriving through a portal stands
// exactly on it.
func standsIn(r sim.Rect, x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

// handOff is the order of a map change (page 3): tell the player where
// they go, checkpoint them as already there (a crash in between still
// lands them on the target), drop them from this instance, and have the
// World join them to the target from its own goroutine. The player's
// in-memory state (HP, MP, EXP, level) travels with them untouched.
func (i *Instance) handOff(p *player, target *gamedata.Map, x, y float64) {
	p.sink.Send(model.Teleport{Tick: i.tick, MapID: target.ID, X: x, Y: y})
	moved := p.Player
	moved.X, moved.Y = x, y
	sink := p.sink
	p.state.X, p.state.Y = x, y
	i.checkpointOn(p, target.ID)
	i.tellParty(p, target.ID)
	i.detach(p.ID)
	i.opts.Transfer.Transfer(moved, sink, target.ID)
}
