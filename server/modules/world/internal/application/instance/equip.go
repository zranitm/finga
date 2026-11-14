package instance

import (
	"context"
	"fmt"
	"time"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/world/internal/model"
	"finga/pkg/transient"
)

// equipTimeout bounds one equip or unequip call to the item service.
const equipTimeout = 5 * time.Second

// Equip wears an inventory item, checked against the player's live level and class, and returns once the new stats apply, or ctx ends.
func (i *Instance) Equip(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, equipSlot string) error {
	return i.askEquip(ctx, equipMsg{id: id, conn: conn, equip: &model.EquipCall{Key: key, Item: item, EquipSlot: equipSlot}})
}

// Unequip puts a worn item back in the Equip tab, in toSlot or, nil, the first free slot, and returns once the new stats apply.
func (i *Instance) Unequip(ctx context.Context, id kernel.CharacterID, conn, key, equipSlot string, toSlot *int) error {
	return i.askEquip(ctx, equipMsg{id: id, conn: conn, unequip: &model.UnequipCall{Key: key, EquipSlot: equipSlot, ToSlot: toSlot}})
}

func (i *Instance) askEquip(ctx context.Context, m equipMsg) error {
	m.done = make(chan error, 1)
	return i.ask(ctx, m, m.done)
}

// ask posts m and waits for its outcome on done, ctx's end, or the map stopping.
func (i *Instance) ask(ctx context.Context, m message, done <-chan error) error {
	if err := i.post(m); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-i.stopped:
		return model.ErrStopped
	}
}

// startEquip hands the call to a worker with the player's live level, class and own stats; the player stays on the map until its result settles.
func (i *Instance) startEquip(m equipMsg) {
	p, ok := i.players[m.id]
	switch {
	case !ok || p.Connection != m.conn:
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
		return
	case p.dead:
		m.done <- model.ErrCharacterDead
		return
	}
	var call func(ctx context.Context) (model.Equipped, error) // its values read here, on the tick
	if m.unequip != nil {
		c := *m.unequip
		c.Account, c.Character = p.Account, p.ID
		call = func(ctx context.Context) (model.Equipped, error) { return i.deps.Equipment.Unequip(ctx, c) }
	} else {
		c := *m.equip
		c.Account, c.Character, c.Level, c.Class, c.Stats = p.Account, p.ID, p.Level, p.Class, i.stats(p).Own
		call = func(ctx context.Context) (model.Equipped, error) { return i.deps.Equipment.Equip(ctx, c) }
	}
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		callCtx, cancel := context.WithTimeout(ctx, equipTimeout)
		result, err := call(callCtx)
		cancel()
		select {
		case i.inbox <- equippedMsg{id: m.id, caller: p, result: result, err: err, done: m.done}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		m.done <- fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
		return
	}
	p.itemCalls++
}

// settleEquip applies a result newer than what the player wears, then answers the caller.
func (i *Instance) settleEquip(m equippedMsg) {
	p, ok := i.players[m.id]
	if ok && p == m.caller { // a join again, even through the same connection, starts its own count
		p.itemCalls--
	}
	if ok && m.err == nil && m.result.Worn.Revision > p.Worn.Revision {
		i.putOn(p, m.result)
	}
	m.done <- m.err
}

// putOn takes what the player now wears, re-derives, and tells the player and the party.
func (i *Instance) putOn(p *player, eq model.Equipped) {
	p.Worn = eq.Worn
	p.private = append(p.private, model.InventoryChanged{Tick: i.tick, Revision: eq.Worn.Revision, Slots: eq.Slots, Equipment: eq.Equipment})
	i.restat(p)
	i.tellParty(p, i.m.ID)
}

// derive is the player's derived stats, from their class, level, points and what they wear.
func (i *Instance) derive(p *player) progression.Derived { return i.stats(p).Derived }
