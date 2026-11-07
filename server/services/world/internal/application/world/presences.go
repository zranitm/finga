package world

import (
	"sync"
	"time"

	"finga/internal/kernel"
	"finga/services/world/internal/application/instance"
)

// presence is where a joined character is and the sink of its connection; gen tells one placement from the next.
type presence struct {
	inst    *instance.Instance
	conn    string
	account kernel.AccountID
	sink    instance.Sink
	gen     uint64 // stamped by each change, so a job that waited for the character's lock sees whether the presence it saw still stands
	transit bool   // handed off by its last map, and not yet marked landed on inst
}

// presences is the world's registry of joined characters, which routes the gateway's commands; the character's lock covers every change but the tick's, which only hands a presence off or drops it.
type presences struct {
	mu   sync.Mutex
	by   map[kernel.CharacterID]presence
	next uint64 // the last gen stamped; from the clock, so gens keep rising across restarts
}

func newPresences() presences {
	return presences{by: map[kernel.CharacterID]presence{}}
}

// stamp gives p the next gen, under mu.
func (ps *presences) stamp(p presence) presence {
	ps.next = max(ps.next+1, uint64(time.Now().UnixNano())) //nolint:gosec // the clock is past 1970
	p.gen = ps.next
	return p
}

// get is the character's presence, if any.
func (ps *presences) get(id kernel.CharacterID) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	return p, ok
}

// place records at as the character's presence and returns it stamped.
func (ps *presences) place(id kernel.CharacterID, at presence) presence {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	at = ps.stamp(at)
	ps.by[id] = at
	return at
}

// at is the character's presence, if it joined through conn.
func (ps *presences) at(id kernel.CharacterID, conn string) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	return p, ok && p.conn == conn
}

// handOff marks the character in transit from from to to if conn holds it there, returning the presence stamped anew; from's tick calls it, and as from holds the player, a landing on from not yet marked loses to it.
func (ps *presences) handOff(id kernel.CharacterID, conn string, from, to *instance.Instance) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	if !ok || p.conn != conn || p.inst != from {
		return presence{}, false
	}
	p.inst, p.transit = to, true
	p = ps.stamp(p)
	ps.by[id] = p
	return p, true
}

// landed marks the presence of gen as on its map, unless it was taken off or replaced first.
func (ps *presences) landed(id kernel.CharacterID, gen uint64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	if !ok || p.gen != gen {
		return
	}
	p.transit = false
	ps.by[id] = p
}

// holds reports whether the presence of gen still stands.
func (ps *presences) holds(id kernel.CharacterID, gen uint64) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.by[id].gen == gen && gen != 0
}

// forget drops the presence of gen; false when another stands.
func (ps *presences) forget(id kernel.CharacterID, gen uint64) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.by[id].gen != gen {
		return false
	}
	delete(ps.by, id)
	return true
}

// forgetThrough drops the character's presence through conn, wherever the tick has moved it since.
func (ps *presences) forgetThrough(id kernel.CharacterID, conn string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p, ok := ps.by[id]; ok && p.conn == conn {
		delete(ps.by, id)
	}
}

// drop forgets the character's presence on inst through conn, which that map dropped, landing marked or not; the tick calls it.
func (ps *presences) drop(id kernel.CharacterID, inst *instance.Instance, conn string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p, ok := ps.by[id]; ok && p.inst == inst && p.conn == conn {
		delete(ps.by, id)
	}
}
