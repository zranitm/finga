package instance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/application/promotions"
	"finga/services/world/internal/model"
)

// promoteMsg asks to be promoted to the class the NPC trains; done hears the outcome once it applies.
type promoteMsg struct {
	id    kernel.CharacterID
	conn  string
	npc   string
	class kernel.Class
	done  chan error
}

// giftedMsg is the trainer's gift's answer, for the tick to apply the promotion.
type giftedMsg struct {
	id     kernel.CharacterID
	caller *player // the join that made the call, whose count of calls in flight it ends
	npc    string
	class  kernel.Class
	result model.Inventory
	err    error
	done   chan error
}

func (promoteMsg) message() {}
func (giftedMsg) message()  {}

// Promote moves the character to the class the NPC trains, once the trainer's weapon is given, and returns once it applies.
func (i *Instance) Promote(ctx context.Context, id kernel.CharacterID, conn, npc string, class kernel.Class) error {
	m := promoteMsg{id: id, conn: conn, npc: npc, class: class, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// promote checks the promotion can go, then has a worker ask the item service for the trainer's weapon, whose answer the tick applies.
func (i *Instance) promote(m promoteMsg) {
	p, class, err := i.promotable(m.id, m.conn, m.npc, m.class)
	if err == nil && p.promoting {
		err = fmt.Errorf("%w: a promotion is in flight", transient.ErrUnavailable)
	}
	if err != nil {
		m.done <- err
		return
	}
	call := i.giftOf(p, class)
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		result, err := i.callGift(ctx, call)
		select {
		case i.inbox <- giftedMsg{id: p.ID, caller: p, npc: m.npc, class: class.ID, result: result, err: err, done: m.done}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		m.done <- fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
		return
	}
	p.itemCalls++
	p.promoting = true
}

// promotable is the player and the class the NPC within reach promotes them to, or why not: out of reach, not a class the NPC trains or theirs leads to, or a condition unmet.
func (i *Instance) promotable(id kernel.CharacterID, conn, npcID string, to kernel.Class) (*player, gamedata.Class, error) {
	p, err := i.alive(id, conn)
	if err != nil {
		return nil, gamedata.Class{}, err
	}
	npc, ok := i.reachable(p, npcID)
	if !ok {
		return nil, gamedata.Class{}, model.ErrNpcOutOfReach
	}
	class, err := i.offered(p, npc, to)
	return p, class, err
}

// offered is the class npc promotes p to, when it is to and p meets every condition.
func (i *Instance) offered(p *player, npc gamedata.NPC, to kernel.Class) (gamedata.Class, error) {
	offer, ok := promotions.Offer(i.data, npc, i.promotionOf(p))
	switch {
	case !ok || offer.Class != to:
		return gamedata.Class{}, model.ErrNotYourNextClass
	case !offer.Met():
		return gamedata.Class{}, model.ErrPromotionUnmet
	}
	class, _ := i.data.Class(to)
	return class, nil
}

// promotionOf is what the promotion rules read of p.
func (i *Instance) promotionOf(p *player) promotions.Character {
	return promotions.Character{Class: p.Class, Level: p.Level, Own: i.stats(p).Own, Skills: p.Skills.Points, Log: p.Quests}
}

// giftOf is the trainer's weapon for p, under a key of the character and class, so a retry, or a promotion again after a crash lost it, gives it once. Its roll is the middle of each range, the same on every try.
func (i *Instance) giftOf(p *player, class gamedata.Class) model.GiftCall {
	weapon := class.Promotion.Weapon
	call := model.GiftCall{Account: p.Account, Character: p.ID, Key: uuidv7.Derived(uuid.UUID(p.ID), "promotion:"+class.ID.String()), Template: weapon}
	if tmpl, ok := i.data.Item(weapon); ok {
		roll := progression.Rolled(tmpl.Rolls, func(r gamedata.Range) int { return (r.Min + r.Max) / 2 })
		call.Stats = &roll
	}
	return call
}

// callGift tries the gift until the item service answers or refuses, or handInFor passes, as a hand-in does.
func (i *Instance) callGift(ctx context.Context, call model.GiftCall) (model.Inventory, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, handInFor, fmt.Errorf("%w: the item service didn't answer the trainer's gift in time", transient.ErrUnavailable))
	defer cancel()
	who := []any{"character_id", call.Character, "template_id", call.Template}
	tries := 0
	wait := &backoff.ExponentialBackOff{InitialInterval: handInRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: handInRetryMax}
	return backoff.Retry(ctx, func() (model.Inventory, error) {
		tries++
		tryCtx, cancelTry := context.WithTimeout(ctx, handInTimeout)
		defer cancelTry()
		result, err := i.deps.Gifts.Give(tryCtx, call)
		if err != nil && !errors.Is(err, transient.ErrUnavailable) {
			return result, backoff.Permanent(err)
		}
		return result, err
	}, backoff.WithBackOff(wait), backoff.WithMaxElapsedTime(0), backoff.WithNotify(func(err error, next time.Duration) {
		i.log.WarnContext(ctx, "trainer's gift try failed; trying again", append(who, "try", tries, "next_in", next, "err", err)...)
	}))
}

// settlePromotion promotes the character on this map, whatever its connection, once the trainer's weapon is theirs, checking again that they still may: alive, and meeting every condition, though they may have stepped away from the trainer. A refusal of the gift changed nothing anywhere; a later one keeps the weapon, and promoting again gives no second one.
func (i *Instance) settlePromotion(m giftedMsg) {
	p, ok := i.players[m.id]
	if ok && p == m.caller {
		p.itemCalls--
		p.promoting = false
	}
	if m.err != nil {
		m.done <- m.err
		return
	}
	if !ok {
		m.done <- fmt.Errorf("%w: the character left the map before the promotion applied; promoting again gives no second weapon", transient.ErrUnavailable)
		return
	}
	npc, _ := i.data.NPC(m.npc)
	class, err := i.offered(p, npc, m.class)
	if err == nil && p.dead {
		err = model.ErrCharacterDead
	}
	if err != nil {
		m.done <- err
		return
	}
	if len(m.result.Slots) > 0 {
		p.private = append(p.private, model.InventoryChanged{Tick: i.tick, Revision: m.result.Revision, Slots: m.result.Slots})
	}
	i.promoteTo(p, class)
	m.done <- nil
}

// promoteTo changes p's class, with its attack and base values; HP and MP are restored to the new maximums, as at a level-up, and the change is saved at once.
func (i *Instance) promoteTo(p *player, class gamedata.Class) {
	p.Class, p.class = class.ID, class
	s := i.stats(p)
	p.derived = s.Derived
	p.HP, p.MP = p.derived.MaxHP, p.derived.MaxMP
	p.changed = true
	p.private = append(p.private, model.Promoted{
		Tick: i.tick, ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, Exp: p.Exp, Stats: s.Own,
		HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP, Sheet: s.Sheet,
	})
	i.tellParty(p, i.m.ID)
	i.checkpoint(p)
}
