package instance

import (
	"errors"
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// errPortalHeld is a press that has to wait for the item calls in flight, which its answer waits for too.
var errPortalHeld = errors.New("instance: a portal press waits for an item call")

// enterPortal hands the player off through the portal they stand in, and says why not when they stand in none or are dead. A press during an item call waits for it to settle, as the hand-off must carry its result; the client sends a press once.
func (i *Instance) enterPortal(p *player) error {
	if p.itemCalls > 0 {
		return errPortalHeld
	}
	if p.dead {
		return model.ErrCharacterDead
	}
	for _, portal := range i.m.Portals {
		if !i.standsIn(portal, p.state) {
			continue
		}
		target, ok := i.data.Map(portal.TargetMap)
		if !ok {
			return nil
		}
		k := slices.IndexFunc(target.Portals, func(q gamedata.Portal) bool { return q.Name == portal.TargetPortal })
		if k < 0 {
			return nil
		}
		arrival := target.Portals[k].At
		dest := Dest{ID: target.ID}
		if target.Under == i.m.ID || i.m.Under == target.ID { // the channel with the same number, so a party and those seen going in are there (underground-and-underwater.md section 6)
			dest.Channel, dest.Paired = i.channel, true
		}
		i.handOffTo(p, target, arrival.X, arrival.Z, awayFrom(target, arrival.X, arrival.Z), dest)
		return nil
	}
	return model.ErrNotAtAGate
}

// pendingPortals judges each portal press that waited for an item call, once it settles, and answers it: the player leaves if still alive and in a portal, and else is told so.
func (i *Instance) pendingPortals() {
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		if len(p.portalWaiting) == 0 || p.itemCalls > 0 {
			continue
		}
		waiting := p.portalWaiting
		p.portalWaiting = nil // before the hand-off, whose detach would answer them as left
		var err error
		if i.match == nil {
			err = i.enterPortal(p)
		}
		for _, done := range waiting {
			done <- err
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
	i.handOffTo(p, target, x, z, yaw, Dest{ID: target.ID})
}

// handOffTo is handOff to dest, which may name a region's channel.
func (i *Instance) handOffTo(p *player, target *gamedata.Map, x, z float64, yaw float32, dest Dest) {
	moved := p.Player
	moved.Timers = i.timersOf(p, time.Now())
	i.leave(p, target, x, 0, z, yaw, moved, dest)
}

// leave sends moved off to dest, on target's ground at (x, z) for feet at y, then tells them first what they'd hear on arriving; it checkpoints them there, but one going into a match here, so a crash lands them where they'd be after it.
func (i *Instance) leave(p *player, target *gamedata.Map, x, y, z float64, yaw float32, moved model.Player, dest Dest, first ...model.Event) {
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
	i.tellParty(p, target.ID, 0) // the landing's join tells the channel picked
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
