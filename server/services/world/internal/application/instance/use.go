package instance

import (
	"context"
	"fmt"
	"time"

	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/world/internal/model"
)

// useTimeout bounds one use call to the item service.
const useTimeout = 5 * time.Second

// UseItem drinks a potion and returns once its HP or MP applies, or ctx ends; template is what the client saw in the slot.
func (i *Instance) UseItem(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, template string) error {
	m := useMsg{id: id, conn: conn, call: model.UseCall{Key: key, Item: item, Template: template}, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// startUse checks the potion would restore something, then has a worker take it from the item service; the player stays on the map until its answer settles.
func (i *Instance) startUse(m useMsg) {
	p, ok := i.players[m.id]
	if !ok || p.Connection != m.conn {
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
		return
	}
	if err := i.canUse(p, m.call.Template); err != nil {
		m.done <- err
		return
	}
	c := m.call
	c.Account, c.Character = p.Account, p.ID
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		callCtx, cancel := context.WithTimeout(ctx, useTimeout)
		result, replayed, err := i.deps.Potions.Use(callCtx, c)
		cancel()
		select {
		case i.inbox <- usedMsg{id: m.id, caller: p, call: c, result: result, replayed: replayed, err: err, done: m.done}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		m.done <- fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
		return
	}
	p.itemCalls++
}

// canUse is why the player can't drink the potion now: it isn't one, the player is dead, or it would restore nothing.
func (i *Instance) canUse(p *player, template string) error {
	t, ok := i.data.Item(template)
	switch {
	case !ok || !t.Usable():
		return model.ErrNotUsable
	case p.dead:
		return model.ErrCharacterDead
	case t.HealHP > 0 && p.HP < p.derived.MaxHP, t.HealMP > 0 && p.MP < p.derived.MaxMP:
		return nil
	case t.HealHP > 0:
		return model.ErrHPFull
	default:
		return model.ErrMPFull
	}
}

// settleUse tells the player what the item service took, then restores what the potion does, unless an earlier call under the key already did.
func (i *Instance) settleUse(m usedMsg) {
	p, ok := i.players[m.id]
	if ok && p == m.caller { // a join again, even through the same connection, starts its own count
		p.itemCalls--
	}
	switch {
	case m.err != nil:
		// A call that fails after the item service took the potion (a timeout, a lost reply) heals nothing, and its retry replays without healing, so the potion is lost; a world-side record of applied keys would be lost on a rejoin, and a key replayed after a map change would heal twice.
		m.done <- m.err
	case !ok:
		m.done <- fmt.Errorf("%w: the character left the map before the potion applied", transient.ErrUnavailable)
	default:
		p.private = append(p.private, model.InventoryChanged{Tick: i.tick, Revision: m.result.Revision, Slots: m.result.Slots})
		if m.replayed { // a retry of a use already applied; healing again would heal for free
			m.done <- nil
			return
		}
		// A player who died while the call was out keeps nothing: the potion was spent when the item service took it, and the dead are not healed. Rare, and simpler than giving it back.
		if p.dead {
			m.done <- model.ErrCharacterDead
			return
		}
		i.restore(p, m.call.Template)
		m.done <- nil
	}
}

// restore adds what the potion restores, held to the maximums; HP rides the Delta, MP the StatsChanged.
func (i *Instance) restore(p *player, template string) {
	t, _ := i.data.Item(template)
	p.HP = min(p.HP+t.HealHP, p.derived.MaxHP)
	p.MP = min(p.MP+t.HealMP, p.derived.MaxMP)
	p.changed = true
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, Sheet: i.sheet(p), HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
}
