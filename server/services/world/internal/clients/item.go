package clients

import (
	"context"
	"fmt"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/grpcx"
	"finga/pkg/transient"
	"finga/services/world/internal/model"
)

// Pickups adapts the item service to loot.Pickups.
type Pickups struct{ c itemv1.ItemServiceClient }

// NewPickups adapts the item client.
func NewPickups(c itemv1.ItemServiceClient) Pickups { return Pickups{c: c} }

// Add keys the grant by the drop's ID, so a retry or replay grants it once.
func (p Pickups) Add(ctx context.Context, in model.Pickup) (model.Inventory, error) {
	drop := in.DropID.String()
	res, err := p.c.Add(ctx, &itemv1.AddRequest{
		Actor:          &itemv1.Actor{AccountId: in.Account.String(), CharacterId: in.Character.String()},
		IdempotencyKey: drop,
		TemplateId:     in.Template,
		Quantity:       int32(in.Quantity),
		Cause:          &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: drop},
		Stats:          rollOf(in.Stats),
	})
	if err != nil {
		return model.Inventory{}, err
	}
	out := make([]model.Slot, 0, len(res.GetChanged()))
	for _, s := range res.GetChanged() {
		slot := model.Slot{Tab: gamedata.Tab(s.GetTab()), Slot: int(s.GetSlot())}
		if it := s.GetItem(); it != nil {
			if slot.Item, err = slotItem(it); err != nil {
				return model.Inventory{}, err
			}
		}
		out = append(out, slot)
	}
	return model.Inventory{Revision: res.GetRevision(), Slots: out}, nil
}

// Inventory reads every item the character holds, each as the slot it fills.
func (p Pickups) Inventory(ctx context.Context, id kernel.CharacterID) (model.Inventory, error) {
	res, err := p.c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: id.String()})
	if err != nil {
		return model.Inventory{}, err
	}
	out := make([]model.Slot, 0, len(res.GetItems()))
	for _, it := range res.GetItems() {
		if it.GetEquipSlot() != "" {
			continue // worn, so in no inventory slot
		}
		item, err := slotItem(it)
		if err != nil {
			return model.Inventory{}, err
		}
		out = append(out, model.Slot{Tab: gamedata.Tab(it.GetTab()), Slot: int(it.GetSlot()), Item: item})
	}
	return model.Inventory{Revision: res.GetRevision(), Slots: out}, nil
}

// rollOf is a roll as the item service takes it, nil for none.
func rollOf(s *progression.ItemStats) *itemv1.ItemStats {
	if s == nil {
		return nil
	}
	//nolint:gosec // stats are small
	return &itemv1.ItemStats{
		Attack: int32(s.Attack), Defense: int32(s.Defense), Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
		Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion), CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), MaxHp: int32(s.MaxHP), MaxMp: int32(s.MaxMP),
	}
}

func slotItem(it *itemv1.Item) (*model.SlotItem, error) {
	id, err := kernel.ParseItemID(it.GetId())
	if err != nil {
		return nil, err
	}
	return &model.SlotItem{ID: id, Template: it.GetTemplateId(), Quantity: int(it.GetQuantity()), Stats: rollFrom(it.GetStats())}, nil
}

// rollFrom is a roll as the item service sends it, nil for none.
func rollFrom(s *itemv1.ItemStats) *progression.ItemStats {
	if s == nil {
		return nil
	}
	return &progression.ItemStats{
		Attack: int(s.GetAttack()), Defense: int(s.GetDefense()), STR: int(s.GetStr()), DEX: int(s.GetDex()), INT: int(s.GetInt()), AGI: int(s.GetAgi()), VIT: int(s.GetVit()),
		Accuracy: int(s.GetAccuracy()), Evasion: int(s.GetEvasion()), CritRate: int(s.GetCritRate()), CritDamage: int(s.GetCritDamage()), MaxHP: int(s.GetMaxHp()), MaxMP: int(s.GetMaxMp()),
	}
}

// Equipment adapts the item service to instance.Equipment and world.Wardrobe; a failure without a reason is transient.ErrUnavailable.
type Equipment struct{ c itemv1.ItemServiceClient }

// NewEquipment adapts the item client.
func NewEquipment(c itemv1.ItemServiceClient) Equipment { return Equipment{c: c} }

// Equip wears an item, checked against the level and class sent.
func (e Equipment) Equip(ctx context.Context, c model.EquipCall) (model.Equipped, error) {
	res, err := e.c.Equip(ctx, &itemv1.EquipRequest{
		Actor: &itemv1.Actor{AccountId: c.Account.String(), CharacterId: c.Character.String()}, IdempotencyKey: c.Key,
		ItemId: c.Item.String(), EquipSlot: c.EquipSlot, Level: int32(c.Level), Class: c.Class.String(), //nolint:gosec // levels are small
	})
	if err != nil {
		return model.Equipped{}, ruleOr(err)
	}
	return equipped(res.GetChanged(), res.GetEquipment(), res.GetWorn(), res.GetRevision())
}

// Unequip puts a worn item back in the Equip tab.
func (e Equipment) Unequip(ctx context.Context, c model.UnequipCall) (model.Equipped, error) {
	req := &itemv1.UnequipRequest{Actor: &itemv1.Actor{AccountId: c.Account.String(), CharacterId: c.Character.String()}, IdempotencyKey: c.Key, EquipSlot: c.EquipSlot}
	if c.ToSlot != nil {
		to := int32(*c.ToSlot) //nolint:gosec // slots are small
		req.ToSlot = &to
	}
	res, err := e.c.Unequip(ctx, req)
	if err != nil {
		return model.Equipped{}, ruleOr(err)
	}
	return equipped(res.GetChanged(), res.GetEquipment(), res.GetWorn(), res.GetRevision())
}

// Worn sums what the character wears, at the inventory's revision.
func (e Equipment) Worn(ctx context.Context, id kernel.CharacterID) (model.Worn, error) {
	res, err := e.c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: id.String()})
	if err != nil {
		return model.Worn{}, ruleOr(err)
	}
	return wornOf(res.GetItems(), res.GetRevision()), nil
}

func equipped(changed []*itemv1.Slot, equipment []*itemv1.EquipSlot, worn []*itemv1.Item, revision uint64) (model.Equipped, error) {
	out := model.Equipped{Worn: wornOf(worn, revision)}
	for _, s := range changed {
		slot := model.Slot{Tab: gamedata.Tab(s.GetTab()), Slot: int(s.GetSlot())}
		if it := s.GetItem(); it != nil {
			item, err := slotItem(it)
			if err != nil {
				return model.Equipped{}, err
			}
			slot.Item = item
		}
		out.Slots = append(out.Slots, slot)
	}
	for _, s := range equipment {
		slot := model.EquipSlot{Slot: s.GetEquipSlot()}
		if it := s.GetItem(); it != nil {
			item, err := slotItem(it)
			if err != nil {
				return model.Equipped{}, err
			}
			slot.Item = item
		}
		out.Equipment = append(out.Equipment, slot)
	}
	return out, nil
}

// wornOf sums the rolls of the worn items among items.
func wornOf(items []*itemv1.Item, revision uint64) model.Worn {
	out := model.Worn{Revision: revision}
	for _, it := range items {
		if r := rollFrom(it.GetStats()); r != nil && it.GetEquipSlot() != "" {
			out.Stats = out.Stats.Plus(*r)
		}
	}
	return out
}

// ruleOr keeps a refusal, with its reason; anything else may pass on a retry.
func ruleOr(err error) error {
	if grpcx.IsRule(err) {
		return err
	}
	return fmt.Errorf("%w: %w", transient.ErrUnavailable, err)
}

// Retryable is a failure without a reason; a rule never passes on retry.
func (Pickups) Retryable(err error) bool { return !grpcx.IsRule(err) }

// Granted is a reused key: an earlier try, under another fingerprint, already granted the drop.
func (Pickups) Granted(err error) bool {
	r, ok := grpcx.ReasonOf(err)
	return ok && r == grpcx.Key(itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED)
}

// Starter adapts the item service to world.Starter.
type Starter struct{ c itemv1.ItemServiceClient }

// NewStarter adapts the item client.
func NewStarter(c itemv1.ItemServiceClient) Starter { return Starter{c: c} }

// GrantStarter asks for the class's starter items, keyed by the character.
func (s Starter) GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) error {
	_, err := s.c.GrantStarter(ctx, &itemv1.GrantStarterRequest{
		Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: id.String()},
		Class: class.String(),
	})
	return err
}
