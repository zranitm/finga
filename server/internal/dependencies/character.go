package dependencies

import (
	"context"

	"finga/internal/application/character"
	"finga/internal/application/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildCharacter adds the character module to h, over the item module.
func buildCharacter(ctx context.Context, h *host.Host, data *gamedata.Data, items *item.Module) (*character.Module, error) {
	m, err := h.Module("character")
	if err != nil {
		return nil, err
	}
	return character.New(ctx, character.Deps{Module: m, Data: data, Item: items})
}
