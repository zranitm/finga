package dependencies

import (
	"context"

	"finga/internal/application/character"
	"finga/internal/application/item"
	repository "finga/internal/repository/character"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildCharacter adds the character module to h, over the item module.
func buildCharacter(ctx context.Context, h *host.Host, data *gamedata.Data, items *item.Module) (*character.Module, error) {
	m, err := h.Module("character")
	if err != nil {
		return nil, err
	}
	pool, dbMetrics, err := m.Database(ctx) // fails fast on an unreachable database
	if err != nil {
		return nil, err
	}
	return character.New(character.Deps{Module: m, Data: data, Item: items, Store: repository.NewPostgres(pool, dbMetrics)})
}
