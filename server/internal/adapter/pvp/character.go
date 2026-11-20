package pvp

import (
	"context"

	"finga/internal/application/character"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// CharacterModule is the character module as the pvp module reads it.
type CharacterModule interface {
	Find(ctx context.Context, ids []kernel.CharacterID) ([]character.Character, error)
	Skills(ctx context.Context, id kernel.CharacterID) ([]character.SkillPoints, error)
}

// Characters is the character module as pvp's port.
type Characters struct{ c CharacterModule }

// NewCharacters adapts c.
func NewCharacters(c CharacterModule) Characters { return Characters{c: c} }

// Character is a character as pvp reads them, of any account: whose, their class, and their world bar and the traits they chose; ErrNoCharacter for none.
func (c Characters) Character(ctx context.Context, id kernel.CharacterID) (model.Character, error) {
	found, err := c.c.Find(ctx, []kernel.CharacterID{id})
	if err != nil {
		return model.Character{}, err
	}
	if len(found) == 0 {
		return model.Character{}, model.ErrNoCharacter
	}
	skills, err := c.c.Skills(ctx, id)
	if err != nil {
		return model.Character{}, err
	}
	out := model.Character{Account: found[0].AccountID, Class: found[0].Class, Bar: found[0].SkillBar, Traits: map[string]string{}}
	for _, s := range skills {
		if s.Trait != "" {
			out.Traits[s.ID] = s.Trait
		}
	}
	return out, nil
}

// Names are the characters by id, those that exist.
func (c Characters) Names(ctx context.Context, ids []kernel.CharacterID) (map[kernel.CharacterID]model.Named, error) {
	found, err := c.c.Find(ctx, ids)
	out := make(map[kernel.CharacterID]model.Named, len(found))
	for _, f := range found {
		out[f.ID] = model.Named{Name: f.Name, Class: f.Class}
	}
	return out, err
}
