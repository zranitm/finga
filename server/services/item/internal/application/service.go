// Package application is the item service's use cases (Low-level design,
// page 11): add, move, discard, the starter grant and the inventory. Every
// change is one transaction through the store's unit of work: the
// idempotency check first, then the rows, the events, and an outbox event,
// so a change can be retried safely and is never half-applied. ports.go
// declares what it needs; the repository layer implements it.
package application

import (
	"context"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/outbox"
	"finga/services/item/internal/model"
)

// AddInput puts a quantity of a template into the inventory: the first
// stack with room, then the first free slot.
type AddInput struct {
	Key        string // idempotency key
	TemplateID string
	Quantity   int
	Cause      model.Cause
}

// MoveInput moves an item to a slot of its tab, or merges it into a stack
// of the same item there.
type MoveInput struct {
	Key    string
	ItemID kernel.ItemID
	Tab    gamedata.Tab
	Slot   int
}

// DiscardInput destroys a whole item or part of a stack.
type DiscardInput struct {
	Key      string
	ItemID   kernel.ItemID
	Quantity int
}

// Result is what a command changed; it is stored for idempotent replays.
type Result struct {
	Changed []model.Slot `json:"changed"`
}

// Options tune the service.
type Options struct {
	Now func() time.Time
}

// Idempotency scopes, one per command.
const (
	scopeAdd     = "item.add"
	scopeMove    = "item.move"
	scopeDiscard = "item.discard"
	scopeStarter = "item.starter"

	topic = "item"
)

// Service is the item service's use cases.
type Service struct {
	store   Store
	content Content
	now     func() time.Time
}

// NewService wires the service over its store and the game data.
func NewService(store Store, content Content, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: store, content: content, now: opts.Now}
}

// Inventory lists the character's items.
func (s *Service) Inventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error) {
	return s.store.Inventory(ctx, owner)
}

// Add puts a quantity of a template into the inventory: the first stack
// with room, overflow into the first free slot of the tab, otherwise
// inventory_full with nothing written.
func (s *Service) Add(ctx context.Context, actor model.Actor, in AddInput) (Result, error) {
	tmpl, ok := s.content.Item(in.TemplateID)
	if !ok {
		return Result{}, model.ErrUnknownItem
	}
	if in.Quantity < 1 || in.Quantity > tmpl.MaxStack {
		return Result{}, model.ErrBadQuantity
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeAdd, in.Key, in, func() (Result, error) {
			return s.add(ctx, uow, actor.Character, tmpl, in.Quantity, in.Cause)
		})
		return err
	})
	return res, err
}

// add is the inventory rule, on the command's unit of work.
func (s *Service) add(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, tmpl gamedata.Item, quantity int, cause model.Cause) (Result, error) {
	inv, err := uow.Items().LockInventory(ctx, owner)
	if err != nil {
		return Result{}, err
	}
	now := s.now()
	var res Result
	left := quantity
	if tmpl.MaxStack > 1 {
		for k := range inv {
			it := &inv[k]
			room := tmpl.MaxStack - it.Quantity
			if it.TemplateID != tmpl.ID || room <= 0 {
				continue
			}
			take := min(room, left)
			it.Quantity += take
			if err := uow.Items().SetQuantity(ctx, it.ID, it.Quantity); err != nil {
				return Result{}, err
			}
			if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventStacked, QuantityDelta: take, ToOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
				return Result{}, err
			}
			res.Changed = append(res.Changed, slotOf(*it))
			left -= take
			if left == 0 {
				break
			}
		}
	}
	if left > 0 {
		slot, ok := freeSlot(inv, tmpl.Tab)
		if !ok {
			return Result{}, model.ErrInventoryFull
		}
		it := model.Item{ID: kernel.NewItemID(), TemplateID: tmpl.ID, Owner: owner, Tab: tmpl.Tab, Slot: slot, Quantity: left, UpdatedAt: now}
		if err := uow.Items().Insert(ctx, it); err != nil {
			return Result{}, err
		}
		if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventCreated, QuantityDelta: left, ToOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
			return Result{}, err
		}
		res.Changed = append(res.Changed, slotOf(it))
	}
	eventType := "ItemGranted"
	if cause.Type == model.CauseDrop {
		eventType = "ItemLooted"
	}
	for _, c := range res.Changed {
		if err := uow.Outbox().Add(ctx, outbox.Event{Topic: topic, Key: c.Item.ID.String(), Type: eventType, Payload: model.OutboxPayload(c.Item, quantity, cause), OccurredAt: now}); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// Move puts an item in a slot of its tab, or merges it into a stack of the
// same item there. Moving onto a different item is a conflict (Stage 1
// has no swap).
func (s *Service) Move(ctx context.Context, actor model.Actor, in MoveInput) (Result, error) {
	if in.Slot < 0 || in.Slot >= model.SlotsPerTab {
		return Result{}, model.ErrBadSlot
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeMove, in.Key, in, func() (Result, error) {
			return s.move(ctx, uow, actor.Character, in)
		})
		return err
	})
	return res, err
}

func (s *Service) move(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in MoveInput) (Result, error) {
	inv, err := uow.Items().LockInventory(ctx, owner)
	if err != nil {
		return Result{}, err
	}
	src := find(inv, func(it model.Item) bool { return it.ID == in.ItemID })
	if src == nil {
		return Result{}, model.ErrNotFound
	}
	if in.Tab != src.Tab {
		return Result{}, model.ErrWrongTab
	}
	if src.Slot == in.Slot {
		return Result{}, nil
	}
	now := s.now()
	from := model.Slot{Tab: src.Tab, Slot: src.Slot}
	dst := find(inv, func(it model.Item) bool { return it.Tab == in.Tab && it.Slot == in.Slot })
	switch {
	case dst == nil:
		src.Slot, src.UpdatedAt = in.Slot, now
		if err := uow.Items().SetSlot(ctx, src.ID, in.Tab, in.Slot); err != nil {
			return Result{}, err
		}
		if err := uow.Events().Append(ctx, model.Event{ItemID: src.ID, Kind: model.EventMoved, FromOwner: &owner, ToOwner: &owner, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
			return Result{}, err
		}
		return Result{Changed: []model.Slot{from, slotOf(*src)}}, nil
	case dst.TemplateID != src.TemplateID:
		return Result{}, model.ErrDifferentItem
	}
	tmpl, ok := s.content.Item(src.TemplateID)
	if !ok {
		return Result{}, model.ErrUnknownItem
	}
	room := tmpl.MaxStack - dst.Quantity
	if room <= 0 {
		return Result{}, model.ErrDifferentItem
	}
	take := min(room, src.Quantity)
	dst.Quantity += take
	dst.UpdatedAt = now
	if err := uow.Items().SetQuantity(ctx, dst.ID, dst.Quantity); err != nil {
		return Result{}, err
	}
	if err := uow.Events().Append(ctx, model.Event{ItemID: dst.ID, Kind: model.EventStacked, QuantityDelta: take, FromOwner: &owner, ToOwner: &owner, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	if err := uow.Events().Append(ctx, model.Event{ItemID: src.ID, Kind: model.EventSplit, QuantityDelta: -take, FromOwner: &owner, ToOwner: &owner, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	src.Quantity -= take
	src.UpdatedAt = now
	if src.Quantity == 0 {
		if err := uow.Items().Destroy(ctx, src.ID); err != nil {
			return Result{}, err
		}
		return Result{Changed: []model.Slot{from, slotOf(*dst)}}, nil
	}
	if err := uow.Items().SetQuantity(ctx, src.ID, src.Quantity); err != nil {
		return Result{}, err
	}
	return Result{Changed: []model.Slot{slotOf(*src), slotOf(*dst)}}, nil
}

// Discard destroys a whole item or part of a stack.
func (s *Service) Discard(ctx context.Context, actor model.Actor, in DiscardInput) (Result, error) {
	if in.Quantity < 1 {
		return Result{}, model.ErrBadQuantity
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeDiscard, in.Key, in, func() (Result, error) {
			return s.discard(ctx, uow, actor.Character, in)
		})
		return err
	})
	return res, err
}

func (s *Service) discard(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in DiscardInput) (Result, error) {
	it, err := uow.Items().LockOwned(ctx, in.ItemID, owner)
	if err != nil {
		return Result{}, err // no row, also when it isn't yours: ErrNotFound after classify
	}
	if in.Quantity > it.Quantity {
		return Result{}, model.ErrBadQuantity
	}
	now := s.now()
	if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventDiscarded, QuantityDelta: -in.Quantity, FromOwner: &owner, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	if err := uow.Outbox().Add(ctx, outbox.Event{Topic: topic, Key: it.ID.String(), Type: "ItemDiscarded", Payload: model.OutboxPayload(&it, in.Quantity, model.Cause{Type: model.CausePlayer}), OccurredAt: now}); err != nil {
		return Result{}, err
	}
	it.Quantity -= in.Quantity
	it.UpdatedAt = now
	if it.Quantity == 0 {
		if err := uow.Items().Destroy(ctx, it.ID); err != nil {
			return Result{}, err
		}
		return Result{Changed: []model.Slot{{Tab: it.Tab, Slot: it.Slot}}}, nil
	}
	if err := uow.Items().SetQuantity(ctx, it.ID, it.Quantity); err != nil {
		return Result{}, err
	}
	return Result{Changed: []model.Slot{slotOf(it)}}, nil
}

// GrantStarter gives a new character its class's starter items, once:
// the character ID is the idempotency key, so a second call (the character
// service retrying, or the world's safety net on join) returns the first
// call's slots and writes nothing. A class the game data does not have is
// ErrUnknownItem.
func (s *Service) GrantStarter(ctx context.Context, actor model.Actor, class kernel.Class) (Result, error) {
	c, ok := s.content.Class(class)
	if !ok {
		return Result{}, model.ErrUnknownItem
	}
	tmpls := make([]gamedata.Item, 0, len(c.StarterItems))
	for _, g := range c.StarterItems {
		tmpl, ok := s.content.Item(g.Item)
		if !ok {
			return Result{}, model.ErrUnknownItem
		}
		tmpls = append(tmpls, tmpl)
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeStarter, actor.Character.String(), class, func() (Result, error) {
			var all Result
			for k, g := range c.StarterItems {
				r, err := s.add(ctx, uow, actor.Character, tmpls[k], g.Quantity, model.Cause{Type: model.CauseStarter})
				if err != nil {
					return Result{}, err
				}
				all.Changed = append(all.Changed, r.Changed...)
			}
			return all, nil
		})
		return err
	})
	return res, err
}

func slotOf(it model.Item) model.Slot {
	copied := it
	return model.Slot{Tab: it.Tab, Slot: it.Slot, Item: &copied}
}

func find(inv []model.Item, pred func(model.Item) bool) *model.Item {
	for k := range inv {
		if pred(inv[k]) {
			return &inv[k]
		}
	}
	return nil
}

// freeSlot is the lowest unoccupied slot of the tab.
func freeSlot(inv []model.Item, tab gamedata.Tab) (int, bool) {
	used := make([]bool, model.SlotsPerTab)
	for _, it := range inv {
		if it.Tab == tab && it.Slot >= 0 && it.Slot < model.SlotsPerTab {
			used[it.Slot] = true
		}
	}
	for slot, taken := range used {
		if !taken {
			return slot, true
		}
	}
	return 0, false
}
