package instance

import (
	"cmp"
	"math"
	"slices"

	"finga/internal/application/world/mobs"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// partDepth is how far into each other two enemies' bodies may stand once a tick's steps are done: the overlap a walking mob may have before it counts as deep.
const partDepth = 0.25

// foes keeps each tick's living fighters' bodies for the ticks an input's view may name, as the arena keeps the mobs': a step meets an enemy where the mover's screen drew it (pvp.md section 19).
type foes struct {
	kept [mobs.History]keptFoes
	list []sim.Blocker // scratch
	now  sim.Blockers  // scratch, for a shove
}

// keptFoes are a tick's bodies, and for each side the blockers it meets there: the mobs and the other side's bodies, built when first asked for.
type keptFoes struct {
	tick   uint64
	bodies []foeBody
	sides  [2]sim.Blockers
	built  [2]bool
}

type foeBody struct {
	id   kernel.CharacterID
	side int // 1 or 2
	body sim.Blocker
}

// blocker is p's body as a blocker.
func (i *Instance) blocker(p *player) sim.Blocker {
	return sim.Blocker{X: p.state.X, Z: p.state.Z, Radius: i.params.Radius, Bottom: p.state.Y, Top: p.state.Y + i.params.Height}
}

// keepFoes files the tick's living fighters, as the frame about to be sent leaves them; a match's downed are not among them, and a held or stood-in fighter is.
func (i *Instance) keepFoes() {
	kept := &i.foes.kept[i.tick%mobs.History]
	kept.tick, kept.bodies, kept.built = i.tick, kept.bodies[:0], [2]bool{}
	if i.match == nil {
		return
	}
	for _, id := range i.order {
		if p := i.players[id]; !p.dead && i.side(id) != 0 {
			kept.bodies = append(kept.bodies, foeBody{id: id, side: i.side(id), body: i.blocker(p)})
		}
	}
	slices.SortFunc(kept.bodies, func(a, b foeBody) int { return cmp.Compare(a.id.String(), b.id.String()) }) // by id, as the client builds them
}

// blockersFor is what p's step meets: the mobs as p's view saw them and, in a match, the living enemies as that frame left them.
func (i *Instance) blockersFor(p *player) *sim.Blockers {
	t, side := i.seen(p.view), i.side(p.ID)
	kept := &i.foes.kept[t%mobs.History]
	if side == 0 || kept.tick != t || len(kept.bodies) == 0 {
		return i.arena.BlockersAt(t)
	}
	if !kept.built[side-1] {
		list := slices.Concat(i.foes.list[:0], i.arena.BodiesAt(t))
		for _, f := range kept.bodies {
			if f.side != side {
				list = append(list, f.body)
			}
		}
		kept.sides[side-1].Set(list, i.m.Geometry.SizeX, i.m.Geometry.SizeZ)
		kept.built[side-1], i.foes.list = true, list
	}
	return &kept.sides[side-1]
}

// foesNow are p's living enemies where they stand now, for a shove; nil off a match.
func (i *Instance) foesNow(p *player) *sim.Blockers {
	if i.match == nil || i.side(p.ID) == 0 {
		return nil
	}
	list := i.foes.list[:0]
	for _, id := range i.order {
		if o := i.players[id]; !o.dead && i.enemies(p.ID, id) {
			list = append(list, i.blocker(o))
		}
	}
	i.foes.list = list
	i.foes.now.Set(list, i.m.Geometry.SizeX, i.m.Geometry.SizeZ)
	return &i.foes.now
}

// part pushes apart enemies more than partDepth into each other, half each, back to partDepth, each push stopped as a walk is; a body that can't move leaves the rest to the other. Pairs go in id order, and a body in a move that passes through bodies isn't parted until it ends.
func (i *Instance) part() {
	if i.match == nil {
		return
	}
	ids := make([]kernel.CharacterID, 0, len(i.order))
	for _, id := range i.order {
		if p := i.players[id]; !p.dead && i.side(id) != 0 && !p.state.Move.Through {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b kernel.CharacterID) int { return cmp.Compare(a.String(), b.String()) })
	for k, a := range ids {
		for _, b := range ids[k+1:] {
			if i.enemies(a, b) {
				i.partPair(i.players[a], i.players[b])
			}
		}
	}
}

func (i *Instance) partPair(a, b *player) {
	if a.state.Y >= b.state.Y+i.params.Height || b.state.Y >= a.state.Y+i.params.Height { // one is above the other's head
		return
	}
	dx, dz := b.state.X-a.state.X, b.state.Z-a.state.Z
	d := math.Hypot(dx, dz)
	ux, uz, sep := dx/math.Max(d, 1e-9), dz/math.Max(d, 1e-9), d
	if d < 1e-9 {
		ux, uz = 1, 0
	}
	if px, pz, ok := i.leftOf(a.ID, b.ID); ok && px*dx+pz*dz < 0 { // they passed through each other this tick: parted back the way they stood at its start
		ux, uz = px/math.Hypot(px, pz), pz/math.Hypot(px, pz)
		sep = ux*dx + uz*dz
	}
	need := 2*i.params.Radius - partDepth - sep // how far they must come apart
	if need <= 0 {
		return
	}
	moved := func(p *player, by float64, sign float64) float64 {
		before := p.state
		p.state = sim.Push(p.state, sign*ux*by, sign*uz*by, i.geometry(p), i.params)
		if p.state != before {
			p.moved = true
		}
		return math.Hypot(p.state.X-before.X, p.state.Z-before.Z)
	}
	gave := moved(a, need/2, -1)
	rest := need - gave
	took := moved(b, rest, 1)
	if left := rest - took; left > 1e-9 {
		moved(a, left, -1)
	}
}

// leftOf is the vector from a to b as the last tick's frame left them, false when either wasn't in it or they stood on one spot.
func (i *Instance) leftOf(a, b kernel.CharacterID) (x, z float64, ok bool) {
	kept := &i.foes.kept[(i.tick-1)%mobs.History]
	if i.tick == 0 || kept.tick != i.tick-1 {
		return 0, 0, false
	}
	var pa, pb *sim.Blocker
	for k := range kept.bodies {
		switch kept.bodies[k].id {
		case a:
			pa = &kept.bodies[k].body
		case b:
			pb = &kept.bodies[k].body
		}
	}
	if pa == nil || pb == nil || math.Hypot(pb.X-pa.X, pb.Z-pa.Z) < 1e-9 {
		return 0, 0, false
	}
	return pb.X - pa.X, pb.Z - pa.Z, true
}
