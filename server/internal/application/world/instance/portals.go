package instance

import (
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// enterPortal hands the player off through the portal they stand in, if any. A press during an item call waits for it to settle, as the hand-off must carry its result; the client sends a press once.
func (i *Instance) enterPortal(p *player) {
	if p.itemCalls > 0 {
		p.portalPress = true
		return
	}
	p.portalPress = false
	if p.dead {
		return
	}
	for _, portal := range i.m.Portals {
		if !i.standsIn(portal, p.state) {
			continue
		}
		target, ok := i.data.Map(portal.TargetMap)
		if !ok {
			return
		}
		k := slices.IndexFunc(target.Portals, func(q gamedata.Portal) bool { return q.Name == portal.TargetPortal })
		if k < 0 {
			return
		}
		arrival := target.Portals[k].At
		i.handOff(p, target, arrival.X, arrival.Z, awayFrom(target, arrival.X, arrival.Z))
		return
	}
}

// pendingPortals tries each portal press that waited for an item call, once it settles: the player leaves if still alive and in a portal.
func (i *Instance) pendingPortals() {
	for _, id := range slices.Clone(i.order) {
		if p := i.players[id]; p.portalPress && p.itemCalls == 0 && i.match == nil {
			i.enterPortal(p)
		}
	}
}

// standsIn reports whether s has its feet centre in the portal's circle, edge included, within portalHeight of the ground at its centre.
func (i *Instance) standsIn(portal gamedata.Portal, s sim.State) bool {
	ground := groundUnder(i.m, i.params, portal.At.X, portal.At.Z, math.Inf(-1))
	return nav.Distance(nav.Point{X: s.X, Z: s.Z}, nav.Point(portal.At)) <= portal.Radius && math.Abs(s.Y-ground) <= portalHeight
}

// handOff checkpoints the player on the target map before detaching, so a crash still lands them there, on the ground at (x, z).
func (i *Instance) handOff(p *player, target *gamedata.Map, x, z float64, yaw float32) {
	moved := p.Player
	moved.Timers = i.timersOf(p, time.Now())
	i.leave(p, target, x, 0, z, yaw, moved, target.ID)
}

// leave sends moved off to dest, on target's ground at (x, z) for feet at y, then tells them first what they'd hear on arriving; it checkpoints them there, but one going into a match here, so a crash lands them where they'd be after it.
func (i *Instance) leave(p *player, target *gamedata.Map, x, y, z float64, yaw float32, moved model.Player, dest string, first ...model.Event) {
	i.flush(p)
	at := standOn(target, i.params, x, y, z, yaw)
	p.sink.Send(model.Teleport{Tick: i.tick, MapID: target.ID, X: at.X, Y: at.Y, Z: at.Z, Yaw: yaw})
	for _, e := range first {
		p.sink.Send(e)
	}
	moved.X, moved.Y, moved.Z, moved.Yaw = at.X, at.Y, at.Z, yaw
	sink := p.sink
	if moved.Back == nil {
		p.state = at
		i.checkpointOn(p, target.ID)
	} else {
		i.checkpoint(p)
	}
	i.tellParty(p, target.ID)
	i.detach(p.ID)
	i.mover.Transfer(i, moved, sink, dest)
}

// flush sends the player what this tick would have told them: their quests' changes and their own events.
func (i *Instance) flush(p *player) {
	i.tellQuestsOf(p)
	for _, e := range p.private {
		p.sink.Send(e)
	}
	p.private = nil
}
