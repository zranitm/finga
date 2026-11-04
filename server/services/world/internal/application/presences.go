package application

import (
	"sync"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/model"
)

// presence is where a joined character is, and the connection it joined through.
type presence struct {
	inst *Instance
	conn string
}

// presences is the world's registry of joined characters; the gateway's commands are routed through it.
type presences struct {
	mu sync.Mutex
	by map[kernel.CharacterID]presence
}

// place records at as the character's presence and returns the one it replaced.
func (ps *presences) place(id kernel.CharacterID, at presence) (prev presence, had bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	prev, had = ps.by[id]
	ps.by[id] = at
	return prev, had
}

// undo puts back what place replaced, unless another placement came since.
func (ps *presences) undo(id kernel.CharacterID, at, prev presence, had bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.by[id] != at {
		return
	}
	if had {
		ps.by[id] = prev
	} else {
		delete(ps.by, id)
	}
}

// at is the character's presence, if it joined through conn.
func (ps *presences) at(id kernel.CharacterID, conn string) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	return p, ok && p.conn == conn
}

// move puts the character on inst if it is still held by conn; false means a newer join replaced it.
func (ps *presences) move(id kernel.CharacterID, conn string, inst *Instance) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	if !ok || p.conn != conn {
		return false
	}
	ps.by[id] = presence{inst: inst, conn: conn}
	return true
}

// forget drops the character's presence, unless another placement came since.
func (ps *presences) forget(id kernel.CharacterID, at presence) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.by[id] == at {
		delete(ps.by, id)
	}
}

// Leave removes the character if it joined through conn; a Leave from any other connection is a no-op.
func (w *World) Leave(id kernel.CharacterID, conn string) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return nil
	}
	if !at.inst.LeaveConnection(id, conn) {
		return model.ErrBusy
	}
	w.presences.forget(id, at)
	return nil
}

// Input queues held keys.
func (w *World) Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input) error {
	return w.command(id, conn, func(i *Instance) bool { return i.Input(id, seq, in) })
}

// Attack queues a basic attack.
func (w *World) Attack(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *Instance) bool { return i.Attack(id) })
}

// PickUp asks for the nearest eligible drop.
func (w *World) PickUp(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *Instance) bool { return i.PickUp(id) })
}

// EnterPortal takes the portal the character stands in.
func (w *World) EnterPortal(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *Instance) bool { return i.EnterPortal(id) })
}

// Say is a map chat line for everyone on the character's map.
func (w *World) Say(id kernel.CharacterID, conn, name, text string) error {
	return w.command(id, conn, func(i *Instance) bool { return i.Chat(id, name, text) })
}

// command posts to the character's map; one from a connection that does not hold the character is
// ignored, as the instance ignores one for a player it does not have. A full inbox is ErrBusy.
func (w *World) command(id kernel.CharacterID, conn string, post func(*Instance) bool) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return nil
	}
	if !post(at.inst) {
		return model.ErrBusy
	}
	return nil
}
