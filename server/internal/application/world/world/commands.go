package world

import (
	"context"
	"errors"
	"fmt"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
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

// Drop is conn's drop, its tab closed or its network gone, which the server can't tell apart: a match fighter's place is held for the mode's reconnect (pvp.md section 4), and anywhere else, or past what a match holds, it's a Leave.
func (w *World) Drop(ctx context.Context, id kernel.CharacterID, conn string) error {
	unlock, err := w.locks.Lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if w.hold(ctx, id, conn) {
		return nil
	}
	_, _, err = w.takeOff(ctx, id, func(p presence) bool { return p.conn == conn })
	return err
}

// hold has the character's match hold their place through conn's drop; false off a match, or where the match holds none. The caller holds the character's lock.
func (w *World) hold(ctx context.Context, id kernel.CharacterID, conn string) bool {
	p, ok := w.presences.at(id, conn)
	if _, fighting := w.fighting.of(id); !ok || !fighting { // most drops are off a match, and skip asking a map
		return false
	}
	answerCtx, cancel := answerOf(ctx)
	defer cancel()
	var held bool
	err := whileBusy(answerCtx, func() (err error) {
		held, err = p.inst.Hold(answerCtx, id, conn)
		return err
	})
	return err == nil && held
}

// Input queues held keys, with the tick of the mobs the client drew.
func (w *World) Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input, view uint32) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Input(id, seq, in, view) })
}

// Attack queues a basic attack aimed along a yaw.
func (w *World) Attack(id kernel.CharacterID, conn string, aim float32, target model.EntityRef) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Attack(id, aim, target) })
}

// EnterPortal takes the portal the character stands in, where they were as of input seq, and returns once it is judged.
func (w *World) EnterPortal(ctx context.Context, id kernel.CharacterID, conn string, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.EnterPortal(ctx, id, seq) })
}

// Say is a map chat line for everyone on the character's map.
func (w *World) Say(id kernel.CharacterID, conn, name, text string) error {
	return w.command(id, conn, func(i *instance.Instance) error { return i.Chat(id, name, text) })
}

// Equip wears an item on the character's map and returns once its stats apply there.
func (w *World) Equip(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, equipSlot string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Equip(ctx, id, conn, key, item, equipSlot) })
}

// TakeLoot takes picked items from the player's own pouches, and returns what each pick came to.
func (w *World) TakeLoot(ctx context.Context, id kernel.CharacterID, conn string, picks []model.Pick, seq uint32) (answers []model.PickAnswer, err error) {
	err = w.wait(id, conn, func(i *instance.Instance) error {
		answers, err = i.TakeLoot(ctx, id, conn, picks, seq)
		return err
	})
	return answers, err
}

// SetLook wears a look and returns once everyone sees it.
func (w *World) SetLook(ctx context.Context, id kernel.CharacterID, conn, key string, look model.Look) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.SetLook(ctx, id, conn, key, look) })
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

// ChooseTrait sets the trait one of the character's skills is used with and returns once the new sheet applies.
func (w *World) ChooseTrait(ctx context.Context, id kernel.CharacterID, conn, skill, trait string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.ChooseTrait(ctx, id, conn, skill, trait) })
}

// UseSkill uses one of the character's active skills, pressed during input seq, and returns once the tick resolves it.
func (w *World) UseSkill(ctx context.Context, id kernel.CharacterID, conn string, use model.SkillUse, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.UseSkill(ctx, id, conn, use, seq) })
}

// Teleport starts a teleport channel to the waypoint, pressed during input seq, and returns once it has started or been refused.
func (w *World) Teleport(ctx context.Context, id kernel.CharacterID, conn, waypoint string, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Teleport(ctx, id, conn, waypoint, seq) })
}

// Promote moves the character to the class the NPC within reach trains, once the trainer's weapon is given, and returns once it applies.
func (w *World) Promote(ctx context.Context, id kernel.CharacterID, conn, npc string, class kernel.Class, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Promote(ctx, id, conn, npc, class, seq) })
}

// ResetPoints returns every spent point to unspent with the free reset and returns once the new stats apply.
func (w *World) ResetPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.ResetPoints(ctx, id, conn, expected) })
}

// Talk sends the character the dialog of an NPC within reach on its map.
func (w *World) Talk(ctx context.Context, id kernel.CharacterID, conn, npc string, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.Talk(ctx, id, conn, npc, seq) })
}

// AcceptQuest takes a quest the NPC within reach gives, and returns once it is in the log.
func (w *World) AcceptQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.AcceptQuest(ctx, id, conn, npc, quest, seq) })
}

// AbandonQuest drops an active quest and its progress.
func (w *World) AbandonQuest(ctx context.Context, id kernel.CharacterID, conn, quest string) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.AbandonQuest(ctx, id, conn, quest) })
}

// HandInQuest hands a ready quest in to the NPC within reach, and returns once the item module's answer applies.
func (w *World) HandInQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error { return i.HandInQuest(ctx, id, conn, npc, quest, seq) })
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
