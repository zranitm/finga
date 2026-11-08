package world

import (
	"context"
	"errors"
	"fmt"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/transient"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

// Leave takes the character off its map, even mid-transfer, if conn holds it, under the character's lock; it returns once its checkpoint is queued.
func (w *World) Leave(ctx context.Context, id kernel.CharacterID, conn string) error {
	unlock, err := w.locks.Lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	_, _, err = w.takeOff(ctx, id, func(p presence) bool { return p.conn == conn })
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

// PickUp asks for the nearest eligible drop; the pickup's trace links to ctx's span.
func (w *World) PickUp(ctx context.Context, id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.PickUp(ctx, id) })
}

// EnterPortal takes the portal the character stands in.
func (w *World) EnterPortal(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.EnterPortal(id) })
}

// Say is a map chat line for everyone on the character's map.
func (w *World) Say(id kernel.CharacterID, conn, name, text string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Chat(id, name, text) })
}

// Equip wears an item on the character's map and returns once its stats apply there.
func (w *World) Equip(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, equipSlot string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Equip(ctx, id, conn, key, item, equipSlot) })
}

// Unequip puts a worn item back in the Equip tab and returns once the stats apply.
func (w *World) Unequip(ctx context.Context, id kernel.CharacterID, conn, key, equipSlot string, toSlot *int) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Unequip(ctx, id, conn, key, equipSlot, toSlot) })
}

// wait runs a command whose caller waits for its outcome, so one it can't take is unavailable, for the caller to retry.
func (w *World) wait(id kernel.CharacterID, conn string, run func(*instance.Instance) error) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return fmt.Errorf("%w: the connection doesn't hold the character", transient.ErrUnavailable)
	}
	err := run(at.inst)
	if errors.Is(err, model.ErrBusy) {
		return fmt.Errorf("%w: the map's inbox is full", transient.ErrUnavailable)
	}
	return err
}

// command posts to the character's map; one from a connection not holding the character is ignored, not an error.
func (w *World) command(id kernel.CharacterID, conn string, post func(*instance.Instance) error) error {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return nil
	}
	return post(at.inst)
}
