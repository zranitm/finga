package loot

import (
	"maps"
	"slices"
	"time"

	"finga/internal/kernel"
)

// turnIdle is how long a map keeps a party's turn it no longer uses; then the party starts again from its first member.
const turnIdle = 10 * time.Minute

// turn is a party's place in its round of drops on this map: the last member given one, and the members in join order then.
type turn struct {
	last  kernel.CharacterID
	order []kernel.CharacterID
	used  uint64 // the tick it last gave a drop
}

// next is the owner of a kill's next drop: the first eligible member after the party's last recipient, in join order; the killer alone, or when no member is eligible.
func (g *Ground) next(k Kill) Owner {
	members := k.Party.Members
	if k.Party.ID == "" || len(members) == 0 {
		return k.Killer
	}
	t := g.turns[k.Party.ID]
	if t == nil {
		t = &turn{}
		g.turns[k.Party.ID] = t
	}
	from := t.from(members)
	for j := range members {
		id := members[(from+j)%len(members)]
		if e := slices.IndexFunc(k.Eligible, func(o Owner) bool { return o.ID == id }); e >= 0 {
			t.last, t.order, t.used = id, members, g.tick
			return k.Eligible[e]
		}
	}
	return k.Killer
}

// from is where in members the round goes on: just after the last recipient, or, if they left, at the first after them who stayed.
func (t *turn) from(members []kernel.CharacterID) int {
	if t.last.IsZero() {
		return 0
	}
	if i := slices.Index(members, t.last); i >= 0 {
		return i + 1
	}
	at := slices.Index(t.order, t.last)
	for j := 1; j < len(t.order); j++ {
		if i := slices.Index(members, t.order[(at+j)%len(t.order)]); i >= 0 {
			return i
		}
	}
	return 0
}

// forgetIdleTurns drops the turns of parties that have not taken a drop here for a while.
func (g *Ground) forgetIdleTurns() {
	maps.DeleteFunc(g.turns, func(_ string, t *turn) bool { return g.tick-t.used > g.turnIdle })
}
