package instance

import (
	"context"
	"fmt"
	"time"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/world/internal/model"
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
	if err := i.post(m); err != nil {
		return err
	}
	select {
	case err := <-m.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-i.stopped:
		return model.ErrStopped
	}
}

// startEquip hands the call to a worker with the player's live level and class; the player stays on the map until its result settles.
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
		c.Account, c.Character, c.Level, c.Class, c.Stats = p.Account, p.ID, p.Level, p.Class, p.Stats
		call = func(ctx context.Context) (model.Equipped, error) { return i.deps.Equipment.Equip(ctx, c) }
	}
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		callCtx, cancel := context.WithTimeout(ctx, equipTimeout)
		result, err := call(callCtx)
		cancel()
		select {
		case i.inbox <- equippedMsg{id: m.id, conn: m.conn, result: result, err: err, done: m.done}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		m.done <- fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
		return
	}
	p.equipping++
}

// settleEquip applies a result newer than what the player wears, then answers the caller.
func (i *Instance) settleEquip(m equippedMsg) {
	p, ok := i.players[m.id]
	if ok && p.Connection == m.conn {
		p.equipping--
	}
	if ok && m.err == nil && m.result.Worn.Revision > p.Worn.Revision {
		i.putOn(p, m.result)
	}
	m.done <- m.err
}

// putOn takes what the player now wears, re-derives, lowers HP and MP to any new maximum, and tells the player and the party.
func (i *Instance) putOn(p *player, eq model.Equipped) {
	p.Worn = eq.Worn
	p.derived = i.derive(p)
	p.HP, p.MP = min(p.HP, p.derived.MaxHP), min(p.MP, p.derived.MaxMP)
	p.changed = true
	p.private = append(p.private,
		model.InventoryChanged{Tick: i.tick, Revision: eq.Worn.Revision, Slots: eq.Slots, Equipment: eq.Equipment},
		model.StatsChanged{Tick: i.tick, Sheet: i.sheet(p), HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP},
	)
	i.tellParty(p, i.m.ID)
}

// derive is the player's derived stats, from their class, level, own stats and what they wear.
func (i *Instance) derive(p *player) progression.Derived {
	return progression.DeriveStats(i.data.StatRules(), p.Class, p.Level, p.Stats, p.Worn.Stats)
}

// sheet is what the player's character window shows.
func (i *Instance) sheet(p *player) progression.Sheet {
	return progression.SheetOf(i.data.StatRules(), p.Class, p.Level, p.Stats, p.Worn.Stats)
}
