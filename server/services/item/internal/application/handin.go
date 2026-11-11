package application

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/idempotency"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/model"
)

// HandInInput takes what a quest's collect objectives ask for and gives its item rewards, for the completion-th time the quest is handed in.
type HandInInput struct {
	Quest      string
	Completion int
	Take       []model.Grant
	Give       []Give
}

// Give is one reward line; equipment comes one per line, with its roll.
type Give struct {
	TemplateID string
	Quantity   int
	Stats      *progression.ItemStats
}

// HandIn is one completion of a quest handed in, as kept for good.
type HandIn struct {
	Quest       string
	Completion  int
	ID          uuid.UUID // the cause ID of the hand-in's events
	Fingerprint []byte    // of the lines it was handed in with
	Result      Result
}

// Completion names one completion of a quest.
type Completion struct {
	Quest      string
	Completion int
}

// MaxHandInLines is the most take and give lines one HandIn takes; MaxHandedIn, the most completions one HandedIn asks about.
const (
	MaxHandInLines = 32
	MaxHandedIn    = 32
)

// The rolls stay out: a repeat rolls again. A repeat with other lines still gets the stored reply, and the fingerprint only tells it apart in the log.
func (in HandInInput) fingerprint(c kernel.CharacterID) []byte {
	values := []string{c.String(), in.Quest, strconv.Itoa(in.Completion)}
	for _, t := range in.Take {
		values = append(values, "take", t.Item, strconv.Itoa(t.Quantity))
	}
	for _, g := range in.Give {
		values = append(values, "give", g.TemplateID, strconv.Itoa(g.Quantity))
	}
	return idempotency.Fingerprint("item.hand_in/1", values...)
}

// HandIn takes what a quest asks for, smallest stacks first and never worn items, then gives its rewards as Add places them; either side failing changes nothing.
// Each completion is handed in once: a repeat, however late and whatever its lines, changes nothing and gets the stored reply.
func (s *Service) HandIn(ctx context.Context, actor model.Actor, in HandInInput) (Result, error) {
	if in.Quest == "" || in.Completion < 1 {
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
	fp := in.fingerprint(actor.Character)
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		stored, found, err := uow.HandIns().Claim(ctx, actor.Character, Completion{Quest: in.Quest, Completion: in.Completion})
		if err != nil {
			return err
		}
		if found {
			if !bytes.Equal(stored.Fingerprint, fp) {
				s.log.WarnContext(ctx, "a hand-in repeated with other lines gets its stored reply", "character_id", actor.Character, "quest_id", in.Quest, "completion", in.Completion)
			}
			res = stored.Result
			return nil
		}
		h := HandIn{Quest: in.Quest, Completion: in.Completion, ID: uuidv7.New(), Fingerprint: fp}
		h.Result, err = s.handIn(ctx, uow, actor.Character, in, gives, model.Cause{Type: model.CauseQuest, ID: h.ID, Quest: in.Quest})
		if h.Result, err = revised(ctx, uow, actor.Character, h.Result, err); err != nil {
			return err
		}
		res = h.Result
		return uow.HandIns().Record(ctx, actor.Character, h)
	})
	return res, err
}

// HandedIn is which of the completions asked about the owner handed in, each with its stored reply.
func (s *Service) HandedIn(ctx context.Context, owner kernel.CharacterID, asked []Completion) ([]HandIn, error) {
	if len(asked) > MaxHandedIn {
		return nil, model.ErrBadBatch
	}
	if len(asked) == 0 {
		return nil, nil
	}
	return s.store.HandedIn(ctx, owner, asked)
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
