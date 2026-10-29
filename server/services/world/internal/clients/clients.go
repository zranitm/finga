// Package clients are the world service's adapters for the services it
// calls, in the world module's own terms: the character service (loading
// characters to join, saving checkpoints and level-ups), the item service
// (pickups, and starter items on join) and the social service (the party
// copy the tick reads).
package clients

import (
	"context"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/world/internal/model"
)

// Pickups is the world's Pickups over the item service.
type Pickups struct{ c itemv1.ItemServiceClient }

// NewPickups adapts the item client.
func NewPickups(c itemv1.ItemServiceClient) Pickups { return Pickups{c: c} }

// Add puts a drop in the inventory, keyed and caused by the drop's ID, so a
// retry or a replay grants it once.
func (p Pickups) Add(ctx context.Context, in model.Pickup) ([]model.Slot, error) {
	drop := in.DropID.String()
	res, err := p.c.Add(ctx, &itemv1.AddRequest{
		Actor:          &itemv1.Actor{AccountId: in.Account.String(), CharacterId: in.Character.String()},
		IdempotencyKey: drop,
		TemplateId:     in.Template,
		Quantity:       int32(in.Quantity),
		Cause:          &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: drop},
	})
	if err != nil {
		return nil, err
	}
	out := make([]model.Slot, 0, len(res.GetChanged()))
	for _, s := range res.GetChanged() {
		slot := model.Slot{Tab: gamedata.Tab(s.GetTab()), Slot: int(s.GetSlot())}
		if it := s.GetItem(); it != nil {
			id, err := kernel.ParseItemID(it.GetId())
			if err != nil {
				return nil, err
			}
			slot.Item = &model.SlotItem{ID: id, Template: it.GetTemplateId(), Quantity: int(it.GetQuantity())}
		}
		out = append(out, slot)
	}
	return out, nil
}

// Retryable is a failure that carries no reason: the item service was not
// reached, or could not explain it. A reason is a rule; retrying cannot help.
func (Pickups) Retryable(err error) bool { return !grpcx.IsRule(err) }

// Saver is the world's Saver over the character service.
type Saver struct {
	c characterv1.CharacterServiceClient
}

// NewSaver adapts the character client.
func NewSaver(c characterv1.CharacterServiceClient) Saver { return Saver{c: c} }

// SaveCheckpoint saves position, HP, MP and EXP.
func (s Saver) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	_, err := s.c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{
		CharacterId: cp.ID.String(), MapId: cp.MapID, X: cp.X, Y: cp.Y, Hp: int32(cp.HP), Mp: int32(cp.MP), Exp: cp.Exp,
	})
	return err
}

// SaveLevelUp saves a level-up.
func (s Saver) SaveLevelUp(ctx context.Context, lu model.LevelUpSave) error {
	st := lu.Stats
	_, err := s.c.SaveLevelUp(ctx, &characterv1.SaveLevelUpRequest{
		CharacterId: lu.ID.String(), Level: int32(lu.Level), Exp: lu.Exp,
		Stats: &characterv1.Stats{Strength: int32(st.STR), Dexterity: int32(st.DEX), Intelligence: int32(st.INT), Agility: int32(st.AGI), Vitality: int32(st.VIT)},
		Hp:    int32(lu.HP), Mp: int32(lu.MP),
	})
	return err
}

// Retryable is a failure that carries no reason.
func (Saver) Retryable(err error) bool { return !grpcx.IsRule(err) }

// Characters is the world's Characters over the character service.
type Characters struct {
	c characterv1.CharacterServiceClient
}

// NewCharacters adapts the character client.
func NewCharacters(c characterv1.CharacterServiceClient) Characters { return Characters{c: c} }

// Load is a character of the account, as last saved; someone else's fails
// with the character service's not-found reason, passed through.
func (c Characters) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	res, err := c.c.Load(ctx, &characterv1.LoadRequest{CharacterId: id.String(), AccountId: account.String()})
	if err != nil {
		return model.Character{}, err
	}
	ch := res.GetCharacter()
	class, err := kernel.ParseClass(ch.GetClass())
	if err != nil {
		return model.Character{}, err
	}
	st := ch.GetStats()
	return model.Character{
		ID: id, Account: account, Name: ch.GetName(), Class: class, Level: int(ch.GetLevel()), Exp: ch.GetExp(),
		Stats: gamedata.Stats{STR: int(st.GetStrength()), DEX: int(st.GetDexterity()), INT: int(st.GetIntelligence()), AGI: int(st.GetAgility()), VIT: int(st.GetVitality())},
		HP:    int(ch.GetHp()), MP: int(ch.GetMp()), MapID: ch.GetMapId(), X: ch.GetX(), Y: ch.GetY(),
	}, nil
}

// Starter is the world's Starter over the item service.
type Starter struct{ c itemv1.ItemServiceClient }

// NewStarter adapts the item client.
func NewStarter(c itemv1.ItemServiceClient) Starter { return Starter{c: c} }

// GrantStarter asks for the class's starter items, keyed by the character:
// a no-op unless creation never granted them.
func (s Starter) GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) error {
	_, err := s.c.GrantStarter(ctx, &itemv1.GrantStarterRequest{
		Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: id.String()},
		Class: class.String(),
	})
	return err
}
