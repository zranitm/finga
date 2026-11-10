package application

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/idempotency"
	"finga/services/item/internal/model"
)

// HandInInput takes what a quest's collect objectives ask for and gives its item rewards.
type HandInInput struct {
	Key   string // the world derives it from the character, the quest and the completion count
	Quest string
	Take  []model.Grant
	Give  []Give
}

// Give is one reward line; equipment comes one per line, with its roll.
type Give struct {
	TemplateID string
	Quantity   int
	Stats      *progression.ItemStats
}

// MaxHandInLines is the most take and give lines one HandIn takes.
const MaxHandInLines = 32

const scopeHandIn = "item.hand_in"

// The rolls stay out: the same hand-in repeated after a crash rolls again, and must still get its stored reply.
func (in HandInInput) fingerprint(c kernel.CharacterID) []byte {
	values := []string{c.String(), in.Quest}
	for _, t := range in.Take {
		values = append(values, "take", t.Item, strconv.Itoa(t.Quantity))
	}
	for _, g := range in.Give {
		values = append(values, "give", g.TemplateID, strconv.Itoa(g.Quantity))
	}
	return idempotency.Fingerprint("item.hand_in/1", values...)
}

// HandIn takes what a quest asks for, smallest stacks first and never worn items, then gives its rewards as Add places them; either side failing changes nothing.
func (s *Service) HandIn(ctx context.Context, actor model.Actor, in HandInInput) (Result, error) {
	if in.Quest == "" {
		return Result{}, model.ErrBadCause
	}
	if len(in.Take)+len(in.Give) > MaxHandInLines {
		return Result{}, model.ErrBadBatch
	}
	for _, t := range in.Take {
		if _, ok := s.content.Item(t.Item); !ok {
			return Result{}, model.ErrUnknownItem
		}
		if t.Quantity < 1 {
			return Result{}, model.ErrBadQuantity
		}
	}
	gives := make([]gamedata.Item, len(in.Give))
	for k, g := range in.Give {
		tmpl, err := s.addable(AddInput{TemplateID: g.TemplateID, Quantity: g.Quantity, Stats: g.Stats})
		if err != nil {
			return Result{}, err
		}
		gives[k] = tmpl
	}
	cause := model.Cause{Type: model.CauseQuest, ID: keyID(in.Key), Quest: in.Quest}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeHandIn, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.handIn(ctx, uow, actor.Character, in, gives, cause)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

// handIn takes first, so the slots it empties can hold the rewards.
func (s *Service) handIn(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in HandInInput, gives []gamedata.Item, cause model.Cause) (Result, error) {
	inv, err := s.lockInventory(ctx, uow, owner)
	if err != nil {
		return Result{}, err
	}
	now := time.Now()
	var changed []model.Slot
	for _, t := range in.Take {
		var stacks []*model.Item
		held := 0
		for k := range inv {
			if it := &inv[k]; !it.Worn() && it.TemplateID == t.Item && it.Quantity > 0 {
				stacks, held = append(stacks, it), held+it.Quantity
			}
		}
		if held < t.Quantity {
			return Result{}, model.ErrNotEnoughItems
		}
		slices.SortStableFunc(stacks, func(a, b *model.Item) int {
			return cmp.Or(cmp.Compare(a.Quantity, b.Quantity), cmp.Compare(a.Tab, b.Tab), cmp.Compare(a.Slot, b.Slot))
		})
		left := t.Quantity
		for _, it := range stacks {
			take := min(it.Quantity, left)
			if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventHandedIn, QuantityDelta: -take, FromOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
				return Result{}, err
			}
			if err := uow.Notices().Send(ctx, model.Notice{Kind: model.NoticeHandedIn, Item: *it, Quantity: -take, Cause: cause, OccurredAt: now}); err != nil {
				return Result{}, err
			}
			it.Quantity -= take
			if it.Quantity == 0 {
				if err := uow.Items().Destroy(ctx, it.ID); err != nil {
					return Result{}, err
				}
				changed = merged(changed, model.Slot{Tab: it.Tab, Slot: it.Slot})
			} else {
				if err := uow.Items().SetQuantity(ctx, it.ID, it.Quantity); err != nil {
					return Result{}, err
				}
				changed = merged(changed, slotOf(*it))
			}
			if left -= take; left == 0 {
				break
			}
		}
	}
	for k, g := range in.Give {
		res, err := s.add(ctx, uow, owner, gives[k], g.Quantity, g.Stats, cause)
		if err != nil {
			return Result{}, err
		}
		changed = merged(changed, res.Changed...)
	}
	return Result{Changed: changed}, nil
}

// merged puts each slot in out, in place of the one already there for its tab and slot, so each place shows how it ends.
func merged(out []model.Slot, slots ...model.Slot) []model.Slot {
	for _, sl := range slots {
		if k := slices.IndexFunc(out, func(o model.Slot) bool { return o.Tab == sl.Tab && o.Slot == sl.Slot }); k >= 0 {
			out[k] = sl
			continue
		}
		out = append(out, sl)
	}
	return out
}
