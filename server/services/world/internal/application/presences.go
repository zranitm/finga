package application

import (
	"context"
	"sync"

	"finga/internal/kernel"
	"finga/internal/sim"
)

// presence is where a joined character is, the connection it joined through and its account.
type presence struct {
	inst    *Instance
	conn    string
	account kernel.AccountID
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

// take removes the character's presence, if it is the account's, and returns it.
func (ps *presences) take(id kernel.CharacterID, account kernel.AccountID) (presence, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.by[id]
	if !ok || p.account != account {
		return presence{}, false
	}
	delete(ps.by, id)
	return p, true
}

// restore puts back a presence take removed, unless another was placed since.
func (ps *presences) restore(id kernel.CharacterID, at presence) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, ok := ps.by[id]; !ok {
		ps.by[id] = at
	}
}

// holds reports whether the character is still at at.
func (ps *presences) holds(id kernel.CharacterID, at presence) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.by[id] == at
}

// Leave removes the character if it joined through conn, and returns once its map has, with its last
// checkpoint queued, so a join after it starts from there. A Leave from any other connection is a no-op.
func (w *World) Leave(ctx context.Context, id kernel.CharacterID, conn string) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return nil
	}
	left := make(chan struct{})
	if err := at.inst.post(leaveMsg{id: id, conn: conn, done: left}); err != nil {
		return err
	}
	if err := at.inst.wait(ctx, left); err != nil {
		return err
	}
	w.presences.forget(id, at)
	return nil
}

// Input queues held keys.
func (w *World) Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input) error {
	return w.command(id, conn, func(i *Instance) error { return i.Input(id, seq, in) })
}

// Attack queues a basic attack.
func (w *World) Attack(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *Instance) error { return i.Attack(id) })
}

// PickUp asks for the nearest eligible drop.
func (w *World) PickUp(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *Instance) error { return i.PickUp(id) })
}

// EnterPortal takes the portal the character stands in.
func (w *World) EnterPortal(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *Instance) error { return i.EnterPortal(id) })
}

// Say is a map chat line for everyone on the character's map.
func (w *World) Say(id kernel.CharacterID, conn, name, text string) error {
	return w.command(id, conn, func(i *Instance) error { return i.Chat(id, name, text) })
}

// command posts to the character's map; one from a connection that does not hold the character is
// ignored, as the instance ignores one for a player it does not have. A full inbox is ErrBusy.
func (w *World) command(id kernel.CharacterID, conn string, post func(*Instance) error) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return nil
	}
	return post(at.inst)
}
