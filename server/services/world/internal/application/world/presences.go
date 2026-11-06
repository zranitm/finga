package world

import (
	"sync"

	"finga/internal/kernel"
	"finga/services/world/internal/application/instance"
)

// presence is where a joined character is; a set left marks a tombstone, closing once the map has handled the leave.
type presence struct {
	inst    *instance.Instance
	conn    string
	account kernel.AccountID
	left    <-chan struct{}
}

// presences is the world's registry of joined characters; the gateway's commands are routed through it.
type presences struct {
	mu sync.Mutex
	by map[kernel.CharacterID]presence
}

// place records at as the character's presence.
func (ps *presences) place(id kernel.CharacterID, at presence) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.by[id] = at
}

// at is the character's presence, if it joined through conn and is not leaving.
func (ps *presences) at(id kernel.CharacterID, conn string) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	return p, ok && p.conn == conn && p.left == nil
}

// move puts the character on inst if conn still holds it; false when a newer join replaced it, or it is leaving.
func (ps *presences) move(id kernel.CharacterID, conn string, inst *instance.Instance) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	if !ok || p.conn != conn || p.left != nil {
		return false
	}
	ps.by[id] = presence{inst: inst, conn: conn, account: p.account}
	return true
}

// forget drops the character's presence, unless another placement came since; false when one did.
func (ps *presences) forget(id kernel.CharacterID, at presence) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.by[id] != at {
		return false
	}
	delete(ps.by, id)
	return true
}

// drop forgets the character's presence on inst through conn, which that map dropped; a newer one, or a leave under way, stays.
func (ps *presences) drop(id kernel.CharacterID, inst *instance.Instance, conn string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p, ok := ps.by[id]; ok && p.inst == inst && p.conn == conn && p.left == nil {
		delete(ps.by, id)
	}
}

// depart returns the presence if want accepts it, tombstoning a live one with left; a leaving one stays, to wait on.
func (ps *presences) depart(id kernel.CharacterID, want func(presence) bool, left <-chan struct{}) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	if !ok || !want(p) {
		return presence{}, false
	}
	if p.left == nil {
		tomb := p
		tomb.left = left
		ps.by[id] = tomb
	}
	return p, true
}

// restore puts back the live presence a tombstone replaced, unless something replaced the tombstone since.
func (ps *presences) restore(id kernel.CharacterID, tomb, live presence) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.by[id] == tomb {
		ps.by[id] = live
	}
}

// holds reports whether the character is still at at.
func (ps *presences) holds(id kernel.CharacterID, at presence) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.by[id] == at
}
