package local

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/transient"
	"finga/services/item"
	"finga/services/world/internal/model"
)

// ItemModule is the item module as the world calls it.
type ItemModule interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
	Add(ctx context.Context, actor item.Actor, r item.AddRequest) (item.Result, error)
	AddMany(ctx context.Context, actor item.Actor, drops []item.AddRequest) (item.Batch, error)
	Use(ctx context.Context, actor item.Actor, r item.UseRequest) (item.Result, bool, error)
	GrantStarter(ctx context.Context, actor item.Actor) (item.Result, error)
	Equip(ctx context.Context, actor item.Actor, r item.EquipRequest) (item.Result, error)
	Unequip(ctx context.Context, actor item.Actor, r item.UnequipRequest) (item.Result, error)
	HandIn(ctx context.Context, actor item.Actor, r item.HandInRequest) (item.Result, error)
	HandedIn(ctx context.Context, owner kernel.CharacterID, asked []item.Completion) ([]item.HandedIn, error)
}

// Loot adapts the item module to loot.Items.
type Loot struct{ items ItemModule }

// NewLoot adapts the item module.
func NewLoot(items ItemModule) Loot { return Loot{items: items} }

// AddMany grants an owner's drops, each keyed by its drop's own key, so a retried or regrouped call grants each once.
func (l Loot) AddMany(ctx context.Context, in model.GrantCall) (model.Granted, error) {
	drops := make([]item.AddRequest, 0, len(in.Drops))
	for _, d := range in.Drops {
		drops = append(drops, item.AddRequest{Key: d.Key.String(), TemplateID: d.Template, Quantity: d.Quantity, Cause: item.Cause{Type: item.CauseDrop, ID: d.Key}, Stats: cloned(d.Stats)})
	}
	b, err := l.items.AddMany(ctx, item.Actor{Account: in.Account, Character: in.Character}, drops)
	if err != nil {
		return model.Granted{}, err
	}
	out := model.Granted{Added: model.Inventory{Revision: b.Revision, Slots: slotsOf(b.Changed)}}
	for _, o := range b.Outcomes {
		out.Outcomes = append(out.Outcomes, grantOutcome(o))
	}
	return out, nil
}

// grantOutcome is one drop's answer; a key reused under another fingerprint means an earlier try granted the drop.
func grantOutcome(o item.Outcome) model.GrantOutcome {
	switch {
	case o.Err == nil && o.Replayed:
		return model.GrantOutcome{Kind: model.GrantReplayed, Replayed: model.Inventory{Revision: o.Result.Revision, Slots: slotsOf(o.Result.Changed)}}
	case o.Err == nil:
		return model.GrantOutcome{Kind: model.GrantAdded}
	case errors.Is(o.Err, kernel.ErrEquipTabFull), errors.Is(o.Err, kernel.ErrUseTabFull), errors.Is(o.Err, kernel.ErrEtcTabFull):
		return model.GrantOutcome{Kind: model.GrantNoRoom}
	case errors.Is(o.Err, kernel.ErrIdempotencyKeyReused):
		return model.GrantOutcome{Kind: model.GrantReplayed}
	default:
		reason, _ := kernel.ReasonOf(o.Err)
		return model.GrantOutcome{Kind: model.GrantRefused, Reason: reason}
	}
}

// Retryable is a failure that breaks no final rule, a module's own failure among them.
func (Loot) Retryable(err error) bool { return !kernel.IsRule(err) }

// Equipment adapts the item module to instance.Equipment, instance.HandIns, instance.Gifts, instance.Potions, world.HandedIns and world.Wardrobe; a failure that breaks no final rule is transient.ErrUnavailable.
type Equipment struct{ items ItemModule }

// NewEquipment adapts the item module.
func NewEquipment(items ItemModule) Equipment { return Equipment{items: items} }

// Equip wears an item, checked against the level, class and own stats sent.
func (e Equipment) Equip(ctx context.Context, c model.EquipCall) (model.Equipped, error) {
	res, err := e.items.Equip(ctx, item.Actor{Account: c.Account, Character: c.Character}, item.EquipRequest{
		Key: c.Key, ItemID: c.Item, EquipSlot: c.EquipSlot, Level: c.Level, Class: c.Class, Stats: c.Stats,
	})
	if err != nil {
		return model.Equipped{}, ruleOr(err)
	}
	return equipped(res), nil
}

// Unequip puts a worn item back in the Equip tab.
func (e Equipment) Unequip(ctx context.Context, c model.UnequipCall) (model.Equipped, error) {
	res, err := e.items.Unequip(ctx, item.Actor{Account: c.Account, Character: c.Character}, item.UnequipRequest{Key: c.Key, EquipSlot: c.EquipSlot, ToSlot: c.ToSlot})
	if err != nil {
		return model.Equipped{}, ruleOr(err)
	}
	return equipped(res), nil
}

// Use takes one of a usable item from its stack, refused when the item isn't the template named.
func (e Equipment) Use(ctx context.Context, c model.UseCall) (model.Inventory, bool, error) {
	res, replayed, err := e.items.Use(ctx, item.Actor{Account: c.Account, Character: c.Character}, item.UseRequest{Key: c.Key, ItemID: c.Item, Template: c.Template})
	if err != nil {
		return model.Inventory{}, false, ruleOr(err)
	}
	return inventoryOf(res), replayed, nil
}

// HandIn takes what a quest asks to collect and gives its item rewards, once per completion.
func (e Equipment) HandIn(ctx context.Context, c model.HandInCall) (model.Inventory, error) {
	r := item.HandInRequest{Quest: c.Quest, Completion: c.Completion, Take: c.Take}
	for _, g := range c.Give {
		r.Give = append(r.Give, item.Give{TemplateID: g.Template, Quantity: g.Quantity, Stats: cloned(g.Stats)})
	}
	res, err := e.items.HandIn(ctx, item.Actor{Account: c.Account, Character: c.Character}, r)
	if err != nil {
		return model.Inventory{}, ruleOr(err)
	}
	return inventoryOf(res), nil
}

// handedInMax is the most completions one HandedIn may ask about: the item module's MaxHandedIn.
const handedInMax = 32

// Give adds one item under the call's key, as a trainer's gift: a retried call, or one after a crash, gives it once while the item module keeps the key.
func (e Equipment) Give(ctx context.Context, c model.GiftCall) (model.Inventory, error) {
	res, err := e.items.Add(ctx, item.Actor{Account: c.Account, Character: c.Character}, item.AddRequest{
		Key: c.Key.String(), TemplateID: c.Template, Quantity: 1, Cause: item.Cause{Type: item.CausePromotion, ID: c.Key}, Stats: cloned(c.Stats),
	})
	if err != nil {
		return model.Inventory{}, ruleOr(err)
	}
	return inventoryOf(res), nil
}

// HandedIn is which of the completions asked the item module handed in, each with the slots it changed, asked in batches of its limit.
func (e Equipment) HandedIn(ctx context.Context, id kernel.CharacterID, asked []model.Completion) ([]model.HandedIn, error) {
	var out []model.HandedIn
	for part := range slices.Chunk(asked, handedInMax) {
		in := make([]item.Completion, 0, len(part))
		for _, c := range part {
			in = append(in, item.Completion{Quest: c.Quest, Completion: c.Completion})
		}
		got, err := e.items.HandedIn(ctx, id, in)
		if err != nil {
			return nil, ruleOr(err)
		}
		for _, h := range got {
			out = append(out, model.HandedIn{Quest: h.Quest, Completion: h.Completion, Items: inventoryOf(h.Result)})
		}
	}
	return out, nil
}

// Worn is what the character wears, at the inventory's revision.
func (e Equipment) Worn(ctx context.Context, id kernel.CharacterID) (model.Worn, error) {
	inv, err := e.items.Inventory(ctx, id)
	if err != nil {
		return model.Worn{}, ruleOr(err)
	}
	return wornOf(inv.Items, inv.Revision), nil
}

func equipped(res item.Result) model.Equipped {
	out := model.Equipped{Slots: slotsOf(res.Changed), Worn: wornOf(res.Worn, res.Revision)}
	for _, s := range res.Equipment {
		out.Equipment = append(out.Equipment, model.EquipSlot{Slot: s.Slot, Item: slotItem(s.Item)})
	}
	return out
}

// wornOf is the worn items among items, each with its template and roll.
func wornOf(items []item.Item, revision uint64) model.Worn {
	out := model.Worn{Revision: revision}
	for _, it := range items {
		if it.Stats != nil && it.EquipSlot != "" {
			out.Items = append(out.Items, model.WornItem{Template: it.TemplateID, Stats: *it.Stats})
		}
	}
	return out
}

func inventoryOf(res item.Result) model.Inventory {
	return model.Inventory{Revision: res.Revision, Slots: slotsOf(res.Changed)}
}

// slotsOf are slots as the item module answers them; an emptied one has no item.
func slotsOf(in []item.Slot) []model.Slot {
	var out []model.Slot
	for _, s := range in {
		out = append(out, model.Slot{Tab: s.Tab, Slot: s.Slot, Item: slotItem(s.Item)})
	}
	return out
}

func slotItem(it *item.Item) *model.SlotItem {
	if it == nil {
		return nil
	}
	return &model.SlotItem{ID: it.ID, Template: it.TemplateID, Quantity: it.Quantity, Stats: cloned(it.Stats)}
}

// cloned is a copy of a roll, so neither module sees the other change it.
func cloned(s *progression.ItemStats) *progression.ItemStats {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// ruleOr keeps a refusal by a rule; anything else may pass on a retry.
func ruleOr(err error) error {
	if kernel.IsRule(err) {
		return err
	}
	return fmt.Errorf("%w: %w", transient.ErrUnavailable, err)
}

// Starter adapts the item module to world.Starter.
type Starter struct{ items ItemModule }

// NewStarter adapts the item module.
func NewStarter(items ItemModule) Starter { return Starter{items: items} }

// GrantStarter asks for the starter items, keyed by the character.
func (s Starter) GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) error {
	_, err := s.items.GrantStarter(ctx, item.Actor{Account: account, Character: id})
	return err
}
