package bots

import (
	"bytes"
	"math"
	"time"

	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// A battle's roles (pvp.md section 8).
const (
	objectiveNear = 10.0 // metres within which an enemy keeps a bot from its point
	stay          = 2    // of a side's bots in a point it holds, how many stay when no enemy is near
	spotShare     = 0.5  // of a point's radius, within which a bot picks where to stand
	standWithin   = 0.5  // metres from its spot at which a bot stops
)

// role is a bot's point in a battle, and the spot in it it stands on.
type role struct {
	point int
	spot  nav.Point
}

// objective is where the bot goes in a battle while no enemy it can reach stands within objectiveNear: its point's spot, picked at its first decision up after each respawn; false when it fights instead.
func (b *Brain) objective(v View, others []Seen) (nav.Point, bool) {
	me := v.Self
	at := point(me.State)
	for k := range others {
		if o := &others[k]; b.reachable(v, o) && nav.Distance(at, nav.Point{X: o.Body.X, Z: o.Body.Z}) <= objectiveNear {
			return nav.Point{}, false
		}
	}
	t := v.Team
	if t.roles == nil {
		t.roles = map[kernel.CharacterID]role{}
	}
	r, ok := t.roles[me.ID]
	switch {
	case !ok:
		r, _ = b.pick(v, false)
	case b.goesOn(v, others, r):
		if next, found := b.pick(v, true); found {
			r = next
		}
	}
	t.roles[me.ID] = r
	return r.spot, true
}

// pick is the point the bot goes to: the nearest its side doesn't hold, else the nearest contested, else, unless needed alone, the nearest; each taking half its side's bots at most, or any number once every point has its half. false when none is left. A bot going on is counted at the point it leaves, held as it is, so needed never picks it again.
func (b *Brain) pick(v View, needed bool) (role, bool) {
	if r, ok := b.pickUpTo(v, needed, v.Battle.Bots/2); ok || needed {
		return r, ok
	}
	return b.pickUpTo(v, needed, math.MaxInt)
}

// pickUpTo is pick with most of the side's bots a point.
func (b *Brain) pickUpTo(v View, needed bool, most int) (role, bool) {
	me, points := v.Self, v.Battle.Points
	taken := make([]int, len(points))
	for _, r := range v.Team.roles {
		taken[r.point]++
	}
	at := point(me.State)
	best, bestRank, bestD := -1, 0, 0.0
	for k, pt := range points {
		rank := 2
		switch {
		case pt.Holder != me.Side:
			rank = 0
		case pt.Contested:
			rank = 1
		}
		if taken[k] >= most || needed && rank == 2 {
			continue
		}
		if d := nav.Distance(at, nav.Point{X: pt.At.X, Z: pt.At.Z}); best < 0 || rank < bestRank || rank == bestRank && d < bestD {
			best, bestRank, bestD = k, rank, d
		}
	}
	if best < 0 {
		return role{}, false
	}
	return role{point: best, spot: b.spotIn(points[best])}, true
}

// spotIn is a spot in pt the bot stands on, rolled within spotShare of its radius from its middle, the middle if the bot wouldn't fit there.
func (b *Brain) spotIn(pt Point) nav.Point {
	yaw, out := b.rand.Float64()*2*math.Pi, math.Sqrt(b.rand.Float64())*spotShare*pt.Radius
	middle := nav.Point{X: pt.At.X, Z: pt.At.Z}
	spot := nav.Point{X: middle.X + out*math.Sin(yaw), Z: middle.Z + out*math.Cos(yaw)}
	if !b.s.Grid.Fits(spot, b.s.Radius) {
		return middle
	}
	return spot
}

// goesOn is whether the bot leaves its point for another that needs it: its side holds it, uncontested, the bot stands in it, and stay of its side's bots there stand in it ahead of it, by id.
func (b *Brain) goesOn(v View, others []Seen, r role) bool {
	me, pt := v.Self, v.Battle.Points[r.point]
	if pt.Holder != me.Side || pt.Contested || !pt.Holds(me.State.X, me.State.Z) {
		return false
	}
	ahead := 0
	for id, o := range v.Team.roles {
		if s := find(others, id); o.point == r.point && s != nil && s.Up && pt.Holds(s.Body.X, s.Body.Z) && bytes.Compare(id[:], me.ID[:]) < 0 {
			ahead++
		}
	}
	return ahead >= stay
}

// reachable is whether o is an enemy up the bot could reach: in a battle, not in their own base, which the bot's side can't enter.
func (b *Brain) reachable(v View, o *Seen) bool {
	return o.Side != v.Self.Side && o.Up && (v.Battle == nil || !b.s.Map.Bases[o.Side-1].Holds(o.Body.X, o.Body.Z))
}

// toward is the keys that take the bot to spot, none once it stands within standWithin.
func (b *Brain) toward(v View, held sim.Input, spot nav.Point) sim.Input {
	at := point(v.Self.State)
	stalled := b.stuck.Stalled(at, v.Self.State.OnGround, held)
	if nav.Distance(at, spot) <= standWithin {
		return sim.Input{}
	}
	return b.walk.Keys(b.s.Map, b.s.Grid, b.s.Radius, at, spot, time.Duration(v.Tick)*b.s.Tick, stalled)
}
