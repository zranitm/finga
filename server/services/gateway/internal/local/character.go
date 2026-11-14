package local

import (
	"context"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/character"
	"finga/services/gateway/internal/model"
)

// CharacterModule is the character module as the gateway calls it.
type CharacterModule interface {
	Create(ctx context.Context, account kernel.AccountID, name string) (character.Character, error)
	List(ctx context.Context, account kernel.AccountID) ([]character.Character, error)
	SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error
}

// Characters is the application's Characters over the character module.
type Characters struct{ c CharacterModule }

// NewCharacters adapts the character module.
func NewCharacters(c CharacterModule) Characters { return Characters{c: c} }

// List lists the account's characters.
func (c Characters) List(ctx context.Context, account kernel.AccountID) ([]model.Character, error) {
	list, err := c.c.List(ctx, account)
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]model.Character, 0, len(list))
	for _, ch := range list {
		out = append(out, characterOf(ch))
	}
	return out, nil
}

// Create creates a character.
func (c Characters) Create(ctx context.Context, account kernel.AccountID, name string) (model.Character, error) {
	ch, err := c.c.Create(ctx, account, name)
	if err != nil {
		return model.Character{}, wrap(err)
	}
	return characterOf(ch), nil
}

// SetSkillBar stores which skill sits on each key of one of the account's characters.
func (c Characters) SetSkillBar(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string) error {
	return wrap(c.c.SetSkillBar(ctx, id, account, bar))
}

func characterOf(c character.Character) model.Character {
	return model.Character{
		ID: c.ID, Name: c.Name, Class: c.Class.String(), Level: c.Level, Exp: c.Exp, Stats: stats(c.OwnStats),
		HP: c.HP, MapID: c.MapID, SkillBar: c.SkillBar,
	}
}

func stats(s gamedata.Stats) model.Stats {
	return model.Stats{STR: s.STR, DEX: s.DEX, INT: s.INT, AGI: s.AGI, VIT: s.VIT}
}
