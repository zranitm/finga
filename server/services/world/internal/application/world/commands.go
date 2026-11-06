package world

import (
	"context"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
)

// Leave takes the character off its map, even mid-transfer, if conn holds it; it returns once its checkpoint is queued.
func (w *World) Leave(ctx context.Context, id kernel.CharacterID, conn string) error {
	_, err := w.depart(ctx, id, func(p presence) bool { return p.conn == conn })
	return err
}

// Input queues held keys.
func (w *World) Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Input(id, seq, in) })
}

// Attack queues a basic attack.
func (w *World) Attack(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Attack(id) })
}

// PickUp asks for the nearest eligible drop.
func (w *World) PickUp(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.PickUp(id) })
}

// EnterPortal takes the portal the character stands in.
func (w *World) EnterPortal(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.EnterPortal(id) })
}

// Say is a map chat line for everyone on the character's map.
func (w *World) Say(id kernel.CharacterID, conn, name, text string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Chat(id, name, text) })
}

// command posts to the character's map; one from a connection not holding the character is ignored, not an error.
func (w *World) command(id kernel.CharacterID, conn string, post func(*instance.Instance) error) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return nil
	}
	return post(at.inst)
}
