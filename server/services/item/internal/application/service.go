// Package application is the item service's use cases; each change is one idempotent transaction.
package application

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/idempotency"
	"finga/services/item/internal/model"
)

// AddInput puts a quantity of a template into the inventory.
type AddInput struct {
	Key        string // idempotency key
	TemplateID string
	Quantity   int
	Cause      model.Cause
	Stats      *progression.ItemStats // the drop's roll: required for equipment, refused on anything else
}

// MoveInput moves an item to a slot of its tab.
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

// UseInput takes one of a usable item from its stack; Template is what the caller believes it is.
type UseInput struct {
	Key      string
	ItemID   kernel.ItemID
	Template string
}

// EquipInput wears an inventory item.
type EquipInput struct {
	Key       string
	ItemID    kernel.ItemID
	EquipSlot string // rings only: empty, the first empty slot of the item's kind, else the first
	Level     int    // the character's live level, class and own stats, which the item's requirements are checked against
	Class     kernel.Class
	Stats     gamedata.Stats
}

// UnequipInput puts a worn item back in the Equip tab.
type UnequipInput struct {
	Key       string
	EquipSlot string
	ToSlot    *int // nil: the first free slot
}

// Result is what a command changed and the inventory's revision after it, 0 when it changed nothing; the repository stores it for idempotent replays.
type Result struct {
	Changed   []model.Slot
	Equipment []model.WornSlot // the equip slots an equip, an unequip or a starter grant changed
	Worn      []model.Item     // after an equip or unequip, every item worn
	Revision  uint64
}

// A fingerprint is a command's values in fixed order; a value added later goes in only when set, so old retries match.

func (in AddInput) fingerprint(c kernel.CharacterID) []byte {
	values := []string{c.String(), in.TemplateID, strconv.Itoa(in.Quantity), string(in.Cause.Type), in.Cause.ID.String()}
	if s := in.Stats; s != nil {
		values = append(values, "stats")
		for _, v := range []int{
			s.STR, s.DEX, s.INT, s.AGI, s.VIT, s.PhysicalAttack, s.MagicAttack, s.PhysicalDefense, s.MagicDefense, s.MaxHP, s.MaxMP, s.Accuracy, s.Evasion,
			s.CritRate, s.CritDamage, s.Penetration, s.HPRegen, s.MPRegen, s.AttackSpeed, s.CooldownSpeed, s.MoveSpeed,
		} {
			values = append(values, strconv.Itoa(v))
		}
	}
	return idempotency.Fingerprint("item.add/2", values...)
}

func (in MoveInput) fingerprint(c kernel.CharacterID) []byte {
	return idempotency.Fingerprint("item.move/1", c.String(), in.ItemID.String(), string(in.Tab), strconv.Itoa(in.Slot))
}

func (in DiscardInput) fingerprint(c kernel.CharacterID) []byte {
	return idempotency.Fingerprint("item.discard/1", c.String(), in.ItemID.String(), strconv.Itoa(in.Quantity))
}

func (in UseInput) fingerprint(c kernel.CharacterID) []byte {
	return idempotency.Fingerprint("item.use/1", c.String(), in.ItemID.String(), in.Template)
}

// The level, class and own stats stay out: a retry after a level-up or a spend is the same request.
func (in EquipInput) fingerprint(c kernel.CharacterID) []byte {
	return idempotency.Fingerprint("item.equip/1", c.String(), in.ItemID.String(), in.EquipSlot)
}

func (in UnequipInput) fingerprint(c kernel.CharacterID) []byte {
	to := ""
	if in.ToSlot != nil {
		to = strconv.Itoa(*in.ToSlot)
	}
	return idempotency.Fingerprint("item.unequip/1", c.String(), in.EquipSlot, to)
}

const (
	scopeAdd     = "item.add"
	scopeMove    = "item.move"
	scopeDiscard = "item.discard"
	scopeUse     = "item.use"
	scopeEquip   = "item.equip"
	scopeUnequip = "item.unequip"
)

// Service is the item service's use cases.
type Service struct {
	store   Store
	content Content
	log     *slog.Logger
}

// Deps are what the service is built over.
type Deps struct {
	Store   Store
	Content Content
	Logger  *slog.Logger
}

// NewService wires the service over deps.
func NewService(deps Deps) *Service {
	return &Service{store: deps.Store, content: deps.Content, log: deps.Logger}
}

// Inventory lists the character's items, at the revision they show; equipment with no roll reads as its lowest until the owner's next change writes it.
func (s *Service) Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error) {
	inv, err := s.store.Inventory(ctx, owner)
	if err != nil {
		return model.Inventory{}, err
	}
	s.unrolled(inv.Items)
	return inv, nil
}

// lockInventory locks the owner's inventory, writing the lowest roll for equipment that has none.
func (s *Service) lockInventory(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID) ([]model.Item, error) {
	inv, err := uow.Items().LockInventory(ctx, owner)
	if err != nil {
		return nil, err
	}
	for _, it := range s.unrolled(inv) {
		if err := uow.Items().AddRoll(ctx, it.ID, *it.Stats); err != nil {
			return nil, err
		}
		s.log.WarnContext(ctx, "equipment had no roll; it now has its template's lowest", "item_id", it.ID, "template", it.TemplateID)
	}
	return inv, nil
}

// unrolled gives equipment with no roll its template's lowest, the rule migration 00008 used for older gear, and returns those items.
// Only a build from before rolls writes such an item, as when an old and a new item service run against one database.
func (s *Service) unrolled(inv []model.Item) []model.Item {
	var out []model.Item
	for i := range inv {
		tmpl, ok := s.content.Item(inv[i].TemplateID)
		if inv[i].Stats != nil || !ok || tmpl.Tab != gamedata.Equip {
			continue
		}
		lowest := progression.Lowest(tmpl.Rolls)
		inv[i].Stats = &lowest
		out = append(out, inv[i])
	}
	return out
}

// Add fills stacks with room, then the first free slot; a full inventory writes nothing.
func (s *Service) Add(ctx context.Context, actor model.Actor, in AddInput) (Result, error) {
	tmpl, err := s.addable(in)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeAdd, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.add(ctx, uow, actor.Character, tmpl, in.Quantity, in.Stats, in.Cause)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

// addable is the template in adds, unless a rule refuses the add before it runs.
func (s *Service) addable(in AddInput) (gamedata.Item, error) {
	tmpl, ok := s.content.Item(in.TemplateID)
	if !ok {
		return gamedata.Item{}, model.ErrUnknownItem
	}
	if in.Quantity < 1 || in.Quantity > tmpl.MaxStack {
		return gamedata.Item{}, model.ErrBadQuantity
	}
	if !takesRoll(tmpl, in.Stats) {
		return gamedata.Item{}, model.ErrBadRoll
	}
	return tmpl, nil
}

// MaxBatch is the most drops one AddMany takes.
const MaxBatch = 32

// Outcome is one drop's of a batch.
type Outcome struct {
	Err      error  // the rule that refused it; nil when it was granted
	Replayed bool   // granted before, under its key
	Result   Result // a replayed drop's stored reply
}

// Batch is what an AddMany did: each drop's outcome, in order, and the slots its new grants changed at the revision they took.
type Batch struct {
	Outcomes []Outcome
	Changed  []model.Slot // each as it stands after the last grant
	Revision uint64       // 0 when nothing was granted
}

// dropRules are the refusals that are one drop's own; anything else fails the whole batch.
var dropRules = []error{model.ErrUnknownItem, model.ErrBadQuantity, model.ErrBadRoll, model.ErrEquipTabFull, model.ErrUseTabFull, model.ErrEtcTabFull, model.ErrKeyReused}

// AddMany adds each drop as Add would, all for one owner in one transaction: a drop a rule refuses rolls back to its savepoint and stores no reply, and the others carry on.
func (s *Service) AddMany(ctx context.Context, actor model.Actor, drops []AddInput) (Batch, error) {
	if len(drops) == 0 || len(drops) > MaxBatch {
		return Batch{}, model.ErrBadBatch
	}
	var b Batch
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		b = Batch{Outcomes: make([]Outcome, len(drops))}
		// The keys' locks before the owner's, in one order, as Add takes its key's before the owner's: no batch and no Add wait on each other in a circle.
		keys := make([]string, 0, len(drops))
		for _, in := range drops {
			keys = append(keys, in.Key)
		}
		slices.Sort(keys)
		for _, k := range slices.Compact(keys) {
			if err := uow.Idempotency().Lock(ctx, actor.Account, scopeAdd, k); err != nil {
				return err
			}
		}
		// Read once outside the drops' savepoints, so equipment found without a roll keeps the one it is given whatever becomes of the drops.
		if _, err := s.lockInventory(ctx, uow, actor.Character); err != nil {
			return err
		}
		for i, in := range drops {
			out, err := s.addOne(ctx, uow, actor, in, &b.Revision)
			if err != nil {
				return err
			}
			b.Outcomes[i] = out.outcome
			b.Changed = merged(b.Changed, out.granted...)
		}
		return nil
	})
	return b, err
}

type batchOutcome struct {
	outcome Outcome
	granted []model.Slot // the slots a new grant changed
}

// addOne is one drop of a batch under its own savepoint; the first grant that changes something takes the batch's revision, which every later one shares.
func (s *Service) addOne(ctx context.Context, uow UnitOfWork, actor model.Actor, in AddInput, revision *uint64) (batchOutcome, error) {
	tmpl, err := s.addable(in)
	if err != nil {
		return batchOutcome{outcome: Outcome{Err: err}}, nil
	}
	ran, took := false, false
	var res Result
	err = uow.Savepoint(ctx, func() error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeAdd, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			ran = true
			res, err := s.add(ctx, uow, actor.Character, tmpl, in.Quantity, in.Stats, in.Cause)
			if err != nil || len(res.Changed) == 0 {
				return res, err
			}
			if *revision == 0 {
				if *revision, err = uow.Items().NextRevision(ctx, actor.Character); err != nil {
					return Result{}, err
				}
				took = true
			}
			res.Revision = *revision
			return res, nil
		})
		return err
	})
	switch {
	case err == nil && ran:
		return batchOutcome{granted: res.Changed}, nil
	case err == nil:
		return batchOutcome{outcome: Outcome{Replayed: true, Result: res}}, nil
	}
	if took {
		*revision = 0 // undone with the savepoint
	}
	for _, rule := range dropRules {
		if errors.Is(err, rule) {
			return batchOutcome{outcome: Outcome{Err: err}}, nil
		}
	}
	return batchOutcome{}, err
}

// takesRoll reports whether tmpl takes stats as its roll: equipment one within its ranges, anything else none.
func takesRoll(tmpl gamedata.Item, stats *progression.ItemStats) bool {
	if tmpl.Tab != gamedata.Equip {
		return stats == nil
	}
	return stats != nil && stats.Fits(tmpl.Rolls)
}

// add puts quantity of tmpl in the inventory; stats is equipment's roll, nil for anything else.
func (s *Service) add(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, tmpl gamedata.Item, quantity int, stats *progression.ItemStats, cause model.Cause) (Result, error) {
	inv, err := s.lockInventory(ctx, uow, owner)
	if err != nil {
		return Result{}, err
	}
	now := time.Now()
	notice := model.NoticeGranted
	if cause.Type == model.CauseDrop {
		notice = model.NoticeLooted
	}
	record := func(it *model.Item, kind model.EventKind, delta int) error {
		if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: kind, QuantityDelta: delta, ToOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
			return err
		}
		return uow.Notices().Send(ctx, model.Notice{Kind: notice, Item: *it, Quantity: delta, Cause: cause, OccurredAt: now})
	}
	var res Result
	left := quantity
	if tmpl.MaxStack > 1 {
		for k := range inv {
			it := &inv[k]
			room := tmpl.MaxStack - it.Quantity
			if it.Worn() || it.TemplateID != tmpl.ID || room <= 0 {
				continue
			}
			take := min(room, left)
			it.Quantity += take
			if err := uow.Items().SetQuantity(ctx, it.ID, it.Quantity); err != nil {
				return Result{}, err
			}
			if err := record(it, model.EventStacked, take); err != nil {
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
			return Result{}, model.TabFull(tmpl.Tab)
		}
		it := model.Item{ID: kernel.NewItemID(), TemplateID: tmpl.ID, Owner: owner, Tab: tmpl.Tab, Slot: slot, Quantity: left, Stats: stats}
		if err := uow.Items().Insert(ctx, it); err != nil {
			return Result{}, err
		}
		if err := record(&it, model.EventCreated, left); err != nil {
			return Result{}, err
		}
		res.Changed = append(res.Changed, slotOf(it))
	}
	return res, nil
}

// Move puts an item in a slot of its tab, merging into a matching stack; there is no swap.
func (s *Service) Move(ctx context.Context, actor model.Actor, in MoveInput) (Result, error) {
	if in.Slot < 0 || in.Slot >= model.SlotsPerTab {
		return Result{}, model.ErrBadSlot
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeMove, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.move(ctx, uow, actor.Character, in)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

func (s *Service) move(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in MoveInput) (Result, error) {
	inv, err := s.lockInventory(ctx, uow, owner)
	if err != nil {
		return Result{}, err
	}
	src := find(inv, func(it model.Item) bool { return it.ID == in.ItemID })
	if src == nil {
		return Result{}, model.ErrNotFound
	}
	if src.Worn() {
		return Result{}, model.ErrItemWorn
	}
	if in.Tab != src.Tab {
		return Result{}, model.ErrWrongTab
	}
	if src.Slot == in.Slot {
		return Result{}, nil
	}
	now := time.Now()
	from := model.Slot{Tab: src.Tab, Slot: src.Slot}
	dst := find(inv, func(it model.Item) bool { return it.Tab == in.Tab && it.Slot == in.Slot })
	switch {
	case dst == nil:
		src.Slot = in.Slot
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
		return Result{}, model.ErrStackFull
	}
	take := min(room, src.Quantity)
	dst.Quantity += take
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
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeDiscard, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.discard(ctx, uow, actor.Character, in)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

func (s *Service) discard(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in DiscardInput) (Result, error) {
	it, err := uow.Items().LockOwned(ctx, in.ItemID, owner)
	if err != nil {
		return Result{}, err // someone else's item is ErrNotFound too
	}
	if in.Quantity > it.Quantity {
		return Result{}, model.ErrBadQuantity
	}
	now := time.Now()
	if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventDiscarded, QuantityDelta: -in.Quantity, FromOwner: &owner, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	if err := uow.Notices().Send(ctx, model.Notice{Kind: model.NoticeDiscarded, Item: it, Quantity: -in.Quantity, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	it.Quantity -= in.Quantity
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

// Use takes one of a usable item from its stack, once per key; what it does, such as the HP a potion restores, the caller applies.
func (s *Service) Use(ctx context.Context, actor model.Actor, in UseInput) (Result, error) {
	if t, ok := s.content.Item(in.Template); !ok || !t.Usable() {
		return Result{}, model.ErrNotUsable
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeUse, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.use(ctx, uow, actor.Character, in)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

func (s *Service) use(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in UseInput) (Result, error) {
	it, err := uow.Items().LockOwned(ctx, in.ItemID, owner)
	if err != nil {
		return Result{}, err // someone else's item is ErrNotFound too
	}
	if it.TemplateID != in.Template {
		return Result{}, model.ErrNotUsable
	}
	now := time.Now()
	if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventUsed, QuantityDelta: -1, FromOwner: &owner, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	if err := uow.Notices().Send(ctx, model.Notice{Kind: model.NoticeUsed, Item: it, Quantity: -1, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	it.Quantity--
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

// Equip wears an inventory item; a worn item in that slot swaps into the inventory slot the new one left, so a swap works with a full tab.
func (s *Service) Equip(ctx context.Context, actor model.Actor, in EquipInput) (Result, error) {
	if in.EquipSlot != "" {
		if _, ok := s.content.EquipSlot(in.EquipSlot); !ok {
			return Result{}, model.ErrWrongEquipSlot
		}
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeEquip, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.equip(ctx, uow, actor.Character, in)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

func (s *Service) equip(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in EquipInput) (Result, error) {
	inv, err := s.lockInventory(ctx, uow, owner)
	if err != nil {
		return Result{}, err
	}
	it := find(inv, func(it model.Item) bool { return it.ID == in.ItemID })
	if it == nil {
		return Result{}, model.ErrNotFound
	}
	if it.Worn() {
		return Result{}, model.ErrItemWorn
	}
	tmpl, ok := s.content.Item(it.TemplateID)
	if !ok {
		return Result{}, model.ErrUnknownItem
	}
	if tmpl.Tab != gamedata.Equip {
		return Result{}, model.ErrNotEquipment
	}
	slot, err := equipSlotFor(s.content.EquipSlots(), tmpl.Slot, in.EquipSlot, inv)
	if err != nil {
		return Result{}, err
	}
	switch progression.Meets(tmpl, in.Level, in.Class, in.Stats) {
	case progression.LevelUnmet:
		return Result{}, model.ErrLevelTooLow
	case progression.ClassUnmet:
		return Result{}, model.ErrWrongClass
	case progression.STRUnmet:
		return Result{}, model.ErrSTRTooLow
	case progression.DEXUnmet:
		return Result{}, model.ErrDEXTooLow
	case progression.INTUnmet:
		return Result{}, model.ErrINTTooLow
	case progression.AGIUnmet:
		return Result{}, model.ErrAGITooLow
	case progression.VITUnmet:
		return Result{}, model.ErrVITTooLow
	case progression.NothingUnmet:
	}
	now, cause := time.Now(), playerCause(in.Key)
	left := model.Slot{Tab: it.Tab, Slot: it.Slot}
	off := find(inv, func(w model.Item) bool { return w.EquipSlot == slot })
	// In before the worn item leaves: one item per equip slot holds at commit.
	if err := uow.Items().SetEquipped(ctx, it.ID, slot); err != nil {
		return Result{}, err
	}
	if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventEquipped, FromOwner: &owner, ToOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
		return Result{}, err
	}
	it.Tab, it.Slot, it.EquipSlot = "", 0, slot
	changed := left
	if off != nil {
		if err := uow.Items().SetSlot(ctx, off.ID, left.Tab, left.Slot); err != nil {
			return Result{}, err
		}
		if err := uow.Events().Append(ctx, model.Event{ItemID: off.ID, Kind: model.EventUnequipped, FromOwner: &owner, ToOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
			return Result{}, err
		}
		off.Tab, off.Slot, off.EquipSlot = left.Tab, left.Slot, ""
		changed = slotOf(*off)
	}
	on := *it
	return Result{Changed: []model.Slot{changed}, Equipment: []model.WornSlot{{Slot: slot, Item: &on}}, Worn: wornOf(inv)}, nil
}

// Unequip puts a worn item in the Equip tab, in the slot asked for or the first free one.
func (s *Service) Unequip(ctx context.Context, actor model.Actor, in UnequipInput) (Result, error) {
	if _, ok := s.content.EquipSlot(in.EquipSlot); !ok {
		return Result{}, model.ErrWrongEquipSlot
	}
	if in.ToSlot != nil && (*in.ToSlot < 0 || *in.ToSlot >= model.SlotsPerTab) {
		return Result{}, model.ErrBadSlot
	}
	var res Result
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeUnequip, in.Key, in.fingerprint(actor.Character), func() (Result, error) {
			res, err := s.unequip(ctx, uow, actor.Character, in)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

func (s *Service) unequip(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, in UnequipInput) (Result, error) {
	inv, err := s.lockInventory(ctx, uow, owner)
	if err != nil {
		return Result{}, err
	}
	off := find(inv, func(it model.Item) bool { return it.EquipSlot == in.EquipSlot })
	if off == nil {
		return Result{}, model.ErrNothingWorn
	}
	to, ok := freeSlot(inv, gamedata.Equip)
	if in.ToSlot != nil {
		to = *in.ToSlot
		if find(inv, func(it model.Item) bool { return it.Tab == gamedata.Equip && it.Slot == to }) != nil {
			return Result{}, model.ErrSlotTaken
		}
	} else if !ok {
		return Result{}, model.ErrEquipTabFull
	}
	if err := uow.Items().SetSlot(ctx, off.ID, gamedata.Equip, to); err != nil {
		return Result{}, err
	}
	if err := uow.Events().Append(ctx, model.Event{ItemID: off.ID, Kind: model.EventUnequipped, FromOwner: &owner, ToOwner: &owner, Cause: playerCause(in.Key), OccurredAt: time.Now()}); err != nil {
		return Result{}, err
	}
	off.Tab, off.Slot, off.EquipSlot = gamedata.Equip, to, ""
	return Result{Changed: []model.Slot{slotOf(*off)}, Equipment: []model.WornSlot{{Slot: in.EquipSlot}}, Worn: wornOf(inv)}, nil
}

// equipSlotFor is where an item of kind goes: the slot asked for, which must take that kind; else the first empty slot of the kind, else its first.
func equipSlotFor(slots []gamedata.EquipSlot, kind gamedata.SlotKind, asked string, inv []model.Item) (string, error) {
	first := ""
	for _, es := range slots {
		if es.Kind != kind {
			continue
		}
		if asked != "" {
			if es.ID == asked {
				return asked, nil
			}
			continue
		}
		if first == "" {
			first = es.ID
		}
		if find(inv, func(it model.Item) bool { return it.EquipSlot == es.ID }) == nil {
			return es.ID, nil
		}
	}
	if first == "" {
		return "", model.ErrWrongEquipSlot
	}
	return first, nil
}

// playerCause is a change the request under key caused.
func playerCause(key string) model.Cause {
	return model.Cause{Type: model.CausePlayer, ID: keyID(key)}
}

// keyID is the UUID of an idempotency key; the handler took the key only as one.
func keyID(key string) uuid.UUID {
	id, _ := uuid.Parse(strings.TrimPrefix(key, "k-"))
	return id
}

// wornOf is every worn item of inv, by equip slot.
func wornOf(inv []model.Item) []model.Item {
	var out []model.Item
	for _, it := range inv {
		if it.Worn() {
			out = append(out, it)
		}
	}
	model.SortInventory(out)
	return out
}

// GrantStarter gives a new character its starter items once, recorded in starter_grants; later calls change nothing. Equipment is worn, unless its slot is taken.
func (s *Service) GrantStarter(ctx context.Context, actor model.Actor, class kernel.Class) (Result, error) {
	c, ok := s.content.Class(class)
	if !ok {
		return Result{}, kernel.ErrUnknownClass
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
		res = Result{} // a rerun starts over
		first, err := uow.Items().RecordStarterGrant(ctx, actor.Character)
		if err != nil || !first {
			return err // a second grant changes nothing
		}
		starter := model.Cause{Type: model.CauseStarter}
		for k, g := range c.StarterItems {
			var stats *progression.ItemStats
			if tmpls[k].Tab == gamedata.Equip {
				lowest := progression.Lowest(tmpls[k].Rolls)
				stats = &lowest
				on, err := s.wear(ctx, uow, actor.Character, tmpls[k], stats, starter)
				if err != nil {
					return err
				}
				if on != nil {
					res.Equipment = append(res.Equipment, *on)
					continue
				}
			}
			r, err := s.add(ctx, uow, actor.Character, tmpls[k], g.Quantity, stats, starter)
			if err != nil {
				return err
			}
			res.Changed = append(res.Changed, r.Changed...)
		}
		res, err = revised(ctx, uow, actor.Character, res, nil)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// wear puts a new equipment item straight into the first empty equip slot of its kind; nil, writing nothing, when every one is taken.
func (s *Service) wear(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, tmpl gamedata.Item, stats *progression.ItemStats, cause model.Cause) (*model.WornSlot, error) {
	inv, err := s.lockInventory(ctx, uow, owner)
	if err != nil {
		return nil, err
	}
	slot, err := equipSlotFor(s.content.EquipSlots(), tmpl.Slot, "", inv)
	if err != nil || find(inv, func(it model.Item) bool { return it.EquipSlot == slot }) != nil {
		return nil, err
	}
	it := model.Item{ID: kernel.NewItemID(), TemplateID: tmpl.ID, Owner: owner, EquipSlot: slot, Quantity: 1, Stats: stats}
	if err := uow.Items().Insert(ctx, it); err != nil {
		return nil, err
	}
	now := time.Now()
	// Created, then equipped, so its last equip event says it is worn.
	for _, ev := range []model.Event{
		{ItemID: it.ID, Kind: model.EventCreated, QuantityDelta: 1, ToOwner: &owner, Cause: cause, OccurredAt: now},
		{ItemID: it.ID, Kind: model.EventEquipped, FromOwner: &owner, ToOwner: &owner, Cause: cause, OccurredAt: now},
	} {
		if err := uow.Events().Append(ctx, ev); err != nil {
			return nil, err
		}
	}
	if err := uow.Notices().Send(ctx, model.Notice{Kind: model.NoticeGranted, Item: it, Quantity: 1, Cause: cause, OccurredAt: now}); err != nil {
		return nil, err
	}
	return &model.WornSlot{Slot: slot, Item: &it}, nil
}

// revised gives a change the owner's next revision, last in its transaction so the revision row's lock is the last it takes; nothing changed takes none.
func revised(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, res Result, err error) (Result, error) {
	if err != nil || len(res.Changed)+len(res.Equipment) == 0 {
		return res, err
	}
	res.Revision, err = uow.Items().NextRevision(ctx, owner)
	if err != nil {
		return Result{}, err
	}
	return res, nil
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
