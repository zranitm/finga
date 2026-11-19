package instance

import (
	"context"
	"fmt"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// lookMsg asks to wear a look; done hears the outcome once it applies.
type lookMsg struct {
	id   kernel.CharacterID
	conn string
	call model.LookCall
	done chan error
}

// lookedMsg is the item module's answer, for the tick to apply.
type lookedMsg struct {
	id     kernel.CharacterID
	caller *player // the join that made the call, whose count of calls in flight it ends
	look   model.Look
	err    error
	done   chan error
}

func (lookMsg) message()   {}
func (lookedMsg) message() {}

// SetLook wears a look, checked by the item module against the account's wardrobe and the player's live class, and returns once everyone sees it.
func (i *Instance) SetLook(ctx context.Context, id kernel.CharacterID, conn, key string, look model.Look) error {
	m := lookMsg{id: id, conn: conn, call: model.LookCall{Key: key, Look: look}, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// startLook refuses a dead player, one being promoted or one in combat, then hands the call to a worker with the player's live class.
func (i *Instance) startLook(m lookMsg) {
	p, err := i.alive(m.id, m.conn)
	if err == nil && p.promoting {
		err = fmt.Errorf("%w: a promotion is in flight", transient.ErrUnavailable)
	}
	if err == nil && i.inCombat(p) {
		err = model.ErrInCombat
	}
	if err != nil {
		m.done <- err
		return
	}
	c := m.call
	c.Account, c.Character, c.Class = p.Account, p.ID, p.Class
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		callCtx, cancel := context.WithTimeout(ctx, equipTimeout)
		look, err := i.deps.Looks.SetLook(callCtx, c)
		cancel()
		select {
		case i.inbox <- lookedMsg{id: m.id, caller: p, look: look, err: err, done: m.done}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		m.done <- fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
		return
	}
	p.itemCalls++
	p.looking++
}

// settleLook wears a look newer than the player's, then answers the caller.
func (i *Instance) settleLook(m lookedMsg) {
	p, ok := i.players[m.id]
	if ok && p == m.caller {
		p.itemCalls--
		p.looking--
	}
	if ok && m.err == nil {
		i.wear(p, m.look)
	}
	m.done <- m.err
}

// wear puts on a look newer than the one p shows, which their next view carries.
func (i *Instance) wear(p *player, look model.Look) {
	if look.Revision > p.Look.Revision {
		p.Look = look
		p.changed = true
	}
}
