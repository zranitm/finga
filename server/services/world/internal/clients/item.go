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

// Loot adapts the item service to loot.Items.
type Loot struct{ c itemv1.ItemServiceClient }

// NewLoot adapts the item client.
func NewLoot(c itemv1.ItemServiceClient) Loot { return Loot{c: c} }

// AddMany grants an owner's drops, each keyed by its drop's own key, so a retried or regrouped call grants each once.
func (l Loot) AddMany(ctx context.Context, in model.GrantCall) (model.Granted, error) {
	req := &itemv1.AddManyRequest{Actor: &itemv1.Actor{AccountId: in.Account.String(), CharacterId: in.Character.String()}}
	for _, d := range in.Drops {
		key := d.Key.String()
		req.Drops = append(req.Drops, &itemv1.AddManyDrop{
			IdempotencyKey: key, TemplateId: d.Template, Quantity: int32(d.Quantity), //nolint:gosec // quantities are small
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: key}, Stats: rollOf(d.Stats),
		})
	}
	res, err := l.c.AddMany(ctx, req)
	if err != nil {
		return model.Granted{}, err
	}
	added, err := changedSlots(res.GetChanged())
	if err != nil {
		return model.Granted{}, err
	}
	out := model.Granted{Added: model.Inventory{Revision: res.GetRevision(), Slots: added}}
	for _, o := range res.GetOutcomes() {
		outcome, err := grantOutcome(o)
		if err != nil {
			return model.Granted{}, err
		}
		out.Outcomes = append(out.Outcomes, outcome)
	}
	return out, nil
}

// grantOutcome is one drop's answer; a key reused under another fingerprint means an earlier try granted the drop.
func grantOutcome(o *itemv1.DropOutcome) (model.GrantOutcome, error) {
	switch o.GetGrant() {
	case itemv1.Grant_GRANT_GRANTED:
		return model.GrantOutcome{Kind: model.GrantAdded}, nil
	case itemv1.Grant_GRANT_ALREADY_GRANTED:
		slots, err := changedSlots(o.GetChanged())
		return model.GrantOutcome{Kind: model.GrantReplayed, Replayed: model.Inventory{Revision: o.GetRevision(), Slots: slots}}, err
	case itemv1.Grant_GRANT_REFUSED:
		switch o.GetReason() {
		case itemv1.Reason_REASON_INVENTORY_FULL:
			return model.GrantOutcome{Kind: model.GrantNoRoom}, nil
		case itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED:
			return model.GrantOutcome{Kind: model.GrantReplayed}, nil
		default:
			return model.GrantOutcome{Kind: model.GrantRefused, Reason: o.GetReason().String()}, nil
		}
	default:
		return model.GrantOutcome{}, fmt.Errorf("a drop's outcome %v is unknown", o.GetGrant())
	}
}

// changedSlots are slots as the item service sends them; an emptied one has no item.
func changedSlots(in []*itemv1.Slot) ([]model.Slot, error) {
	var out []model.Slot
	for _, s := range in {
		slot := model.Slot{Tab: gamedata.Tab(s.GetTab()), Slot: int(s.GetSlot())}
		if it := s.GetItem(); it != nil {
			item, err := slotItem(it)
			if err != nil {
				return nil, err
			}
			slot.Item = item
		}
		out = append(out, slot)
	}
	return out, nil
}

// Retryable is a failure without a reason; a rule never passes on retry.
func (Loot) Retryable(err error) bool { return !grpcx.IsRule(err) }

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
	slots, err := changedSlots(changed)
	if err != nil {
		return model.Equipped{}, err
	}
	out := model.Equipped{Slots: slots, Worn: wornOf(worn, revision)}
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
