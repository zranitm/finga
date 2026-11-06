package clients

import (
	"context"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/world/internal/model"
)

// Saver adapts the character service to world.Saver.
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

// Characters adapts the character service to world.Characters.
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
