// Package clients adapts the character, item and social services to the world's ports.
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

// Add keys the grant by the drop's ID, so a retry or replay grants it once.
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

// Retryable is a failure without a reason; a rule never passes on retry.
func (Pickups) Retryable(err error) bool { return !grpcx.IsRule(err) }

// Granted is a reused key: an earlier try, under another fingerprint, already granted the drop.
func (Pickups) Granted(err error) bool {
	r, ok := grpcx.ReasonOf(err)
	return ok && r == grpcx.Key(itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED)
}

// Saver is the world's Saver over the character service.
type Saver struct {
	c characterv1.CharacterServiceClient
}

// NewSaver adapts the character client.
func NewSaver(c characterv1.CharacterServiceClient) Saver { return Saver{c: c} }

// SaveCheckpoint saves position, HP, MP and the whole progression.
func (s Saver) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	_, err := s.c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{
		CharacterId: cp.ID.String(), MapId: cp.MapID, X: cp.X, Y: cp.Y, Hp: int32(cp.HP), Mp: int32(cp.MP),
		Level: int32(cp.Level), Exp: cp.Exp, Stats: statsOf(cp.Stats),
	})
	return err
}

func statsOf(st gamedata.Stats) *characterv1.Stats {
	return &characterv1.Stats{Strength: int32(st.STR), Dexterity: int32(st.DEX), Intelligence: int32(st.INT), Agility: int32(st.AGI), Vitality: int32(st.VIT)}
}

// Retryable is a failure the character service may yet get past; any other would be retried forever.
func (Saver) Retryable(err error) bool { return grpcx.IsTransient(err) }

// Characters is the world's Characters over the character service.
type Characters struct {
	c characterv1.CharacterServiceClient
}

// NewCharacters adapts the character client.
func NewCharacters(c characterv1.CharacterServiceClient) Characters { return Characters{c: c} }

// Load is the account's character as last saved.
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

// GrantStarter asks for the class's starter items, keyed by the character.
func (s Starter) GrantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) error {
	_, err := s.c.GrantStarter(ctx, &itemv1.GrantStarterRequest{
		Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: id.String()},
		Class: class.String(),
	})
	return err
}
