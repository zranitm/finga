package world

import (
	"context"
	"errors"
	"fmt"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/pkg/transient"
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

// Input queues held keys, with the tick of the mobs the client drew.
func (w *World) Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input, view uint32) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Input(id, seq, in, view) })
}

// Attack queues a basic attack.
func (w *World) Attack(id kernel.CharacterID, conn string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Attack(id) })
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

// UseItem drinks a potion and returns once its HP applies.
func (w *World) UseItem(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, template string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.UseItem(ctx, id, conn, key, item, template) })
}

// SpendPoints adds points to the character's stats and returns once the new stats apply.
func (w *World) SpendPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points gamedata.Stats) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.SpendPoints(ctx, id, conn, expected, points) })
}

// SpendSkillPoints invests skill points in the character's skills and returns once the new sheet applies.
func (w *World) SpendSkillPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points map[string]int) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.SpendSkillPoints(ctx, id, conn, expected, points) })
}

// UseSkill uses one of the character's active skills, pressed during input seq, and returns once the tick resolves it.
func (w *World) UseSkill(ctx context.Context, id kernel.CharacterID, conn, skill string, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.UseSkill(ctx, id, conn, skill, seq) })
}

// Promote moves the character to the class the NPC within reach trains, once the trainer's weapon is given, and returns once it applies.
func (w *World) Promote(ctx context.Context, id kernel.CharacterID, conn, npc string, class kernel.Class) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Promote(ctx, id, conn, npc, class) })
}

// ResetPoints returns every spent point to unspent with the free reset and returns once the new stats apply.
func (w *World) ResetPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.ResetPoints(ctx, id, conn, expected) })
}

// Talk sends the character the dialog of an NPC within reach on its map.
func (w *World) Talk(ctx context.Context, id kernel.CharacterID, conn, npc string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Talk(ctx, id, conn, npc) })
}

// AcceptQuest takes a quest the NPC within reach gives, and returns once it is in the log.
func (w *World) AcceptQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.AcceptQuest(ctx, id, conn, npc, quest) })
}

// AbandonQuest drops an active quest and its progress.
func (w *World) AbandonQuest(ctx context.Context, id kernel.CharacterID, conn, quest string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.AbandonQuest(ctx, id, conn, quest) })
}

// HandInQuest hands a ready quest in to the NPC within reach, and returns once the item module's answer applies.
func (w *World) HandInQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.HandInQuest(ctx, id, conn, npc, quest) })
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
