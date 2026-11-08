package clients

import (
	"context"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/grpcx"
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
	return &model.SlotItem{ID: id, Template: it.GetTemplateId(), Quantity: int(it.GetQuantity())}, nil
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
