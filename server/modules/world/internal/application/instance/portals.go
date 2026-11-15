package instance

import (
	"math"
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
		if !i.standsIn(portal, p.state) {
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
		arrival := target.Portals[k].At
		i.handOff(p, target, arrival.X, arrival.Z, awayFrom(target, arrival.X, arrival.Z))
		return true
	}
	return false
}

// standsIn reports whether s has its feet centre in the portal's circle, its edge included, and within portalHeight of the ground at
// its centre: a character arriving through a portal stands at its centre.
func (i *Instance) standsIn(portal gamedata.Portal, s sim.State) bool {
	ground := groundUnder(i.m, i.params, portal.At.X, portal.At.Z, math.Inf(-1))
	return math.Hypot(s.X-portal.At.X, s.Z-portal.At.Z) <= portal.Radius && math.Abs(s.Y-ground) <= portalHeight
}

// handOff checkpoints the player on the target map before detaching, so a crash still lands them there; it arrives on the ground at
// (x, z), facing yaw.
func (i *Instance) handOff(p *player, target *gamedata.Map, x, z float64, yaw float32) {
	i.tellQuestsOf(p)
	for _, e := range p.private { // this tick's, which the broadcast would have sent
		p.sink.Send(e)
	}
	p.private = nil
	at := standOn(target, i.params, x, 0, z, yaw)
	p.sink.Send(model.Teleport{Tick: i.tick, MapID: target.ID, X: at.X, Y: at.Y, Z: at.Z, Yaw: yaw})
	moved := p.Player
	moved.X, moved.Y, moved.Z, moved.Yaw = at.X, at.Y, at.Z, yaw
	moved.Timers = i.timersOf(p, time.Now())
	sink := p.sink
	p.state = at
	i.checkpointOn(p, target.ID)
	i.tellParty(p, target.ID)
	i.detach(p.ID)
	i.mover.Transfer(i, moved, sink, target.ID)
}
